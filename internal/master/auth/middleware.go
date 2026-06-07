package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/time/rate"

	"github.com/heckertobias/orkestra/internal/master/store"
)

const sessionCookie = "orkestra_session"

// SessionMiddleware wraps an HTTP handler: reads the session cookie or Bearer API key,
// resolves the user, and injects it into the request context.
func SessionMiddleware(q *store.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now().UnixMilli()

			// 1. Try session cookie.
			cookie, err := r.Cookie(sessionCookie)
			if err == nil && cookie.Value != "" {
				sessionID := SessionIDFromRaw(cookie.Value)
				sess, err := q.GetSession(r.Context(), store.GetSessionParams{
					ID:        sessionID,
					ExpiresAt: now,
				})
				if err == nil {
					user, err := q.GetUser(r.Context(), sess.UserID)
					if err == nil && !user.Disabled {
						roles, _ := q.GetUserRoles(r.Context(), user.ID)
						ctx := WithUser(r.Context(), &UserCtx{
							ID:       user.ID,
							Username: user.Username,
							Roles:    roles,
						})
						ctx = WithSessionID(ctx, sessionID)
						go func() {
							_ = q.TouchSession(context.Background(), store.TouchSessionParams{
								ID:       sessionID,
								LastSeen: now,
							})
						}()
						r = r.WithContext(ctx)
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			// 2. Try Bearer API key.
			if bearer := r.Header.Get("Authorization"); strings.HasPrefix(bearer, "Bearer ") {
				rawKey := strings.TrimPrefix(bearer, "Bearer ")
				h := sha256.Sum256([]byte(rawKey))
				keyHash := fmt.Sprintf("%x", h)
				key, err := q.GetAPIKeyByHash(r.Context(), keyHash)
				if err == nil && !key.Revoked {
					if key.ExpiresAt == nil || *key.ExpiresAt > now {
						user, err := q.GetUser(r.Context(), key.UserID)
						if err == nil && !user.Disabled {
							roles, _ := q.GetUserRoles(r.Context(), user.ID)
							ctx := WithUser(r.Context(), &UserCtx{
								ID:       user.ID,
								Username: user.Username,
								Roles:    roles,
							})
							r = r.WithContext(ctx)
							go func() {
								_ = q.TouchAPIKey(context.Background(), key.ID, now)
							}()
						}
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// rateLimiters is a per-IP rate limiter store for auth endpoints.
var rateLimiters = struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
}{entries: make(map[string]*rateLimitEntry)}

type rateLimitEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func getIPLimiter(ip string) *rate.Limiter {
	rateLimiters.mu.Lock()
	defer rateLimiters.mu.Unlock()
	e, ok := rateLimiters.entries[ip]
	if !ok {
		// 5 requests per minute, burst of 10.
		e = &rateLimitEntry{limiter: rate.NewLimiter(rate.Every(12*time.Second), 10)}
		rateLimiters.entries[ip] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

// RateLimitMiddleware wraps an http.HandlerFunc with per-IP rate limiting.
func RateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip = strings.SplitN(xff, ",", 2)[0]
		}
		limiter := getIPLimiter(ip)
		if !limiter.Allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// NewAuthInterceptor is a Connect unary interceptor that returns Unauthenticated
// for all procedures except those in the public set.
func NewAuthInterceptor(publicProcedures map[string]bool) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if publicProcedures[req.Spec().Procedure] {
				return next(ctx, req)
			}
			if UserFromContext(ctx) == nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("not authenticated"))
			}
			return next(ctx, req)
		}
	}
}

// SetSessionCookie adds a Set-Cookie header to the provided header map.
// Used with Connect response headers (which are http.Header, not http.ResponseWriter).
func SetSessionCookie(h http.Header, rawToken string, expires time.Time) {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    rawToken,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	h.Add("Set-Cookie", c.String())
}

// ClearSessionCookie adds a clearing Set-Cookie header.
func ClearSessionCookie(h http.Header) {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	h.Add("Set-Cookie", c.String())
}

// HasRole reports whether the user has any of the given roles.
func HasRole(u *UserCtx, roles ...string) bool {
	for _, r := range u.Roles {
		for _, want := range roles {
			if r == want {
				return true
			}
		}
	}
	return false
}
