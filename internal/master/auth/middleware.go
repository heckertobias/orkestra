package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/heckertobias/orkestra/internal/master/store"
)

const sessionCookie = "orkestra_session"

// SessionMiddleware wraps an HTTP handler: reads the session cookie, resolves the
// user, and injects it into the request context. Non-authenticated requests pass
// through with a nil user — individual handlers enforce auth as needed.
func SessionMiddleware(q *store.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookie)
			if err == nil && cookie.Value != "" {
				sessionID := SessionIDFromRaw(cookie.Value)
				now := time.Now().UnixMilli()
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
						// Touch session asynchronously — don't block the request.
						go func() {
							_ = q.TouchSession(context.Background(), store.TouchSessionParams{
								ID:       sessionID,
								LastSeen: now,
							})
						}()
						r = r.WithContext(ctx)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
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
