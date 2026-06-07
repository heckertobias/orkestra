// Package oidc manages the OIDC/SSO login flow for the Master.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	masterauth "github.com/heckertobias/orkestra/internal/master/auth"
	"github.com/heckertobias/orkestra/internal/master/pki"
	"github.com/heckertobias/orkestra/internal/master/store"
)

const (
	stateCookieName = "orkestra_oidc_state"
	stateTTL        = 10 * time.Minute
)

// Provider wraps the OIDC verifier and OAuth2 config for the SSO login flow.
type Provider struct {
	mu       sync.RWMutex
	verifier *gooidc.IDTokenVerifier
	oauth2   *oauth2.Config
	claims   map[string]string // claim value → role

	db  *store.Queries
	kek []byte
}

// New creates a Provider. Call Reload to initialize from DB.
func New(db *store.Queries, kek []byte) *Provider {
	return &Provider{db: db, kek: kek}
}

// Reload reads the OIDC config from DB and (re)initialises the OIDC verifier.
func (p *Provider) Reload(ctx context.Context, redirectURL string) error {
	cfg, err := p.db.GetOIDCConfig(ctx)
	if err != nil {
		return nil // no config yet — that's fine
	}
	if !cfg.Enabled {
		p.mu.Lock()
		p.verifier = nil
		p.oauth2 = nil
		p.mu.Unlock()
		return nil
	}

	secretEnc, err := base64.StdEncoding.DecodeString(cfg.ClientSecretEnc)
	if err != nil {
		return fmt.Errorf("decode encrypted secret: %w", err)
	}
	secretBytes, err := pki.Decrypt(p.kek, secretEnc)
	if err != nil {
		return fmt.Errorf("decrypt client secret: %w", err)
	}

	provider, err := gooidc.NewProvider(ctx, cfg.IssuerUrl)
	if err != nil {
		return fmt.Errorf("oidc provider %q: %w", cfg.IssuerUrl, err)
	}

	var scopes []string
	if len(cfg.Scopes) > 0 {
		_ = json.Unmarshal(cfg.Scopes, &scopes)
	}
	if len(scopes) == 0 {
		scopes = []string{gooidc.ScopeOpenID, "profile", "email"}
	}

	var claims map[string]string
	if len(cfg.ClaimMapping) > 0 {
		_ = json.Unmarshal(cfg.ClaimMapping, &claims)
	}

	p.mu.Lock()
	p.verifier = provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})
	p.oauth2 = &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: string(secretBytes),
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	}
	p.claims = claims
	p.mu.Unlock()
	return nil
}

// Enabled reports whether OIDC is configured and active.
func (p *Provider) Enabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.verifier != nil
}

// LoginHandler starts the OIDC auth flow (GET /auth/oidc/login).
func (p *Provider) LoginHandler(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	oauth2cfg := p.oauth2
	p.mu.RUnlock()
	if oauth2cfg == nil {
		http.Error(w, "OIDC not configured", http.StatusServiceUnavailable)
		return
	}

	state := randomState()
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   int(stateTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, oauth2cfg.AuthCodeURL(state), http.StatusFound)
}

// CallbackHandler handles the IdP redirect (GET /auth/oidc/callback).
// On success it creates/updates the user and sets a session cookie, then redirects to /.
func (p *Provider) CallbackHandler(q *store.Queries, sessionTTL time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.mu.RLock()
		oauth2cfg := p.oauth2
		verifier := p.verifier
		claimMap := p.claims
		p.mu.RUnlock()

		if oauth2cfg == nil || verifier == nil {
			http.Error(w, "OIDC not configured", http.StatusServiceUnavailable)
			return
		}

		// Validate state.
		stateCookie, err := r.Cookie(stateCookieName)
		if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: stateCookieName, MaxAge: -1, Path: "/"})

		token, err := oauth2cfg.Exchange(r.Context(), r.URL.Query().Get("code"))
		if err != nil {
			slog.Warn("oidc code exchange failed", "err", err)
			http.Error(w, "code exchange failed", http.StatusBadRequest)
			return
		}

		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "no id_token in response", http.StatusBadRequest)
			return
		}
		idToken, err := verifier.Verify(r.Context(), rawIDToken)
		if err != nil {
			http.Error(w, "invalid id_token", http.StatusUnauthorized)
			return
		}

		var claims struct {
			Sub   string `json:"sub"`
			Email string `json:"email"`
			Name  string `json:"name"`
			// Generic map for role claim lookups.
			Extra map[string]interface{} `json:"-"`
		}
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "parse claims", http.StatusInternalServerError)
			return
		}
		var allClaims map[string]interface{}
		_ = idToken.Claims(&allClaims)

		// Resolve role from claim mapping.
		role := resolveRole(allClaims, claimMap)

		ctx := r.Context()
		user, err := upsertOIDCUser(ctx, q, claims.Sub, claims.Email, claims.Name, role)
		if err != nil {
			slog.Error("oidc upsert user", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Create session.
		rawToken, sessionID, err := masterauth.GenerateSessionToken()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		now := time.Now()
		expires := now.Add(sessionTTL)
		if err := q.InsertSession(ctx, store.InsertSessionParams{
			ID:        sessionID,
			UserID:    user.ID,
			CreatedAt: now.UnixMilli(),
			ExpiresAt: expires.UnixMilli(),
			LastSeen:  now.UnixMilli(),
		}); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		masterauth.SetSessionCookie(w.Header(), rawToken, expires)
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func upsertOIDCUser(ctx context.Context, q *store.Queries, sub, email, name, role string) (store.User, error) {
	username := email
	if username == "" {
		username = sub
	}
	displayName := name
	if displayName == "" {
		displayName = username
	}

	// Try to find by OIDC subject.
	existing, err := q.GetUserByOIDCSubject(ctx, sub)
	if err == nil {
		return existing, nil
	}

	// Create new OIDC user.
	user, err := q.InsertOIDCUser(ctx, store.InsertOIDCUserParams{
		Username:    username,
		DisplayName: &displayName,
		OIDCSubject: sub,
		CreatedAt:   time.Now().UnixMilli(),
	})
	if err != nil {
		return store.User{}, fmt.Errorf("insert oidc user: %w", err)
	}

	// Assign role if mapped.
	if role != "" {
		_, _ = q.InsertRoleBinding(ctx, store.InsertRoleBindingParams{
			ID:        randomID(),
			UserID:    user.ID,
			RoleID:    "role-" + role,
			CreatedAt: time.Now().UnixMilli(),
		})
	}
	return user, nil
}

func resolveRole(claims map[string]interface{}, claimMap map[string]string) string {
	for k, role := range claimMap {
		if v, ok := claims[k]; ok {
			if fmt.Sprintf("%v", v) != "" {
				_ = v
				return role
			}
		}
	}
	return ""
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
