// Package oidc manages the OIDC/SSO login flow for the Master.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// errNoAccount is returned by resolveOIDCUser when the IdP identity cannot be
// matched to any pre-created orkestra account.
var errNoAccount = errors.New("no matching orkestra account")

// Provider wraps the OIDC verifier and OAuth2 config for the SSO login flow.
type Provider struct {
	mu          sync.RWMutex
	verifier    *gooidc.IDTokenVerifier
	oauth2      *oauth2.Config
	claims      map[string]string // group value → role
	groupsClaim string            // token claim that holds group membership (default: "groups")

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

	var claimMapping map[string]string
	if len(cfg.ClaimMapping) > 0 {
		_ = json.Unmarshal(cfg.ClaimMapping, &claimMapping)
	}

	groupsClaim := cfg.GroupsClaim
	if groupsClaim == "" {
		groupsClaim = "groups"
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
	p.claims = claimMapping
	p.groupsClaim = groupsClaim
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
// On success it looks up the pre-existing user and sets a session cookie, then redirects to /.
// Unknown users are redirected to /login?error=oidc_no_account.
func (p *Provider) CallbackHandler(q *store.Queries, sessionTTL time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.mu.RLock()
		oauth2cfg := p.oauth2
		verifier := p.verifier
		claimMap := p.claims
		groupsClaim := p.groupsClaim
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
		}
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "parse claims", http.StatusInternalServerError)
			return
		}
		var allClaims map[string]interface{}
		_ = idToken.Claims(&allClaims)

		// Resolve role from group claim mapping.
		role := resolveRole(allClaims, groupsClaim, claimMap)

		ctx := r.Context()
		user, err := resolveOIDCUser(ctx, q, claims.Sub, claims.Email)
		if err != nil {
			if errors.Is(err, errNoAccount) {
				slog.Info("oidc login rejected — no matching account", "email", claims.Email, "sub", claims.Sub)
				http.Redirect(w, r, "/login?error=oidc_no_account", http.StatusFound)
				return
			}
			slog.Error("oidc resolve user", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Idempotently ensure the resolved role binding exists.
		if role != "" {
			roleID := "role-" + role
			bindings, _ := q.ListRoleBindingsByUser(ctx, user.ID)
			hasRole := false
			for _, b := range bindings {
				if b.RoleID == roleID && b.ServerID == nil && b.StackID == nil {
					hasRole = true
					break
				}
			}
			if !hasRole {
				_, _ = q.InsertRoleBinding(ctx, store.InsertRoleBindingParams{
					ID:        randomID(),
					UserID:    user.ID,
					RoleID:    roleID,
					CreatedAt: time.Now().UnixMilli(),
				})
			}
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

// resolveOIDCUser finds the pre-created orkestra user matching the OIDC identity.
// It first tries by oidc_subject, then by email (and links the subject on first match).
// Returns errNoAccount if no matching, non-disabled user is found.
func resolveOIDCUser(ctx context.Context, q *store.Queries, sub, email string) (store.User, error) {
	// Fast path: already linked.
	existing, err := q.GetUserByOIDCSubject(ctx, sub)
	if err == nil {
		return existing, nil
	}

	// Match by email (username field holds the email address).
	if email == "" {
		return store.User{}, errNoAccount
	}
	user, err := q.GetUserByUsername(ctx, email)
	if err != nil || user.Disabled {
		return store.User{}, errNoAccount
	}

	// Link the OIDC subject for future fast-path lookups.
	if err := q.SetOIDCSubject(ctx, user.ID, sub); err != nil {
		slog.Warn("oidc: failed to link subject to user", "user_id", user.ID, "err", err)
	}
	user.OidcSubject = &sub
	return user, nil
}

// resolveRole reads the configured groups claim from the token, normalises it to
// a string slice, and returns the highest-privilege role found in the mapping.
// Privilege order: admin > operator > viewer.
func resolveRole(claims map[string]interface{}, groupsClaim string, claimMap map[string]string) string {
	if len(claimMap) == 0 || groupsClaim == "" {
		return ""
	}
	raw, ok := claims[groupsClaim]
	if !ok {
		return ""
	}

	// Normalise: accept a single string or an array of strings/interfaces.
	var groups []string
	switch v := raw.(type) {
	case string:
		groups = []string{v}
	case []string:
		groups = v
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				groups = append(groups, s)
			}
		}
	}

	// Priority: admin > operator > viewer.
	rolePriority := map[string]int{"admin": 3, "operator": 2, "viewer": 1}
	best := ""
	bestPrio := 0
	for _, g := range groups {
		if role, ok := claimMap[g]; ok {
			if p := rolePriority[role]; p > bestPrio {
				best = role
				bestPrio = p
			}
		}
	}
	return best
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
