// Package auth provides session management, password hashing, and auth context.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (OWASP recommended minimum).
const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

// contextKey is the type for context values set by this package.
type contextKey int

const (
	ctxUser      contextKey = iota
	ctxSessionID contextKey = iota
)

// UserCtx holds the authenticated user from the session.
type UserCtx struct {
	ID       string
	Username string
	Roles    []string
}

// WithUser stores the authenticated user in the context.
func WithUser(ctx context.Context, u *UserCtx) context.Context {
	return context.WithValue(ctx, ctxUser, u)
}

// UserFromContext retrieves the authenticated user from the context.
// Returns nil if not authenticated.
func UserFromContext(ctx context.Context) *UserCtx {
	u, _ := ctx.Value(ctxUser).(*UserCtx)
	return u
}

// WithSessionID stores the session ID in the context for Logout.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxSessionID, id)
}

// SessionIDFromContext retrieves the session ID from the context.
func SessionIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxSessionID).(string)
	return id
}

// HashPassword returns a PHC-format argon2id hash of the password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads, b64Salt, b64Hash), nil
}

// VerifyPassword checks a password against a PHC-format argon2id hash.
func VerifyPassword(encodedHash, password string) bool {
	var m, t, p uint32
	var b64Salt, b64Hash string
	_, err := fmt.Sscanf(encodedHash,
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		&m, &t, &p, &b64Salt, &b64Hash)
	if err != nil {
		return false
	}
	// Sscanf with %s stops at whitespace; but base64 might contain '+'. Use manual split.
	return verifyArgon2id(encodedHash, password, m, t, p)
}

func verifyArgon2id(encoded, password string, m, t, p uint32) bool {
	// Parse properly: split on '$'
	// Format: $argon2id$v=19$m=M,t=T,p=P$SALT$HASH
	parts := splitDollar(encoded)
	if len(parts) < 5 {
		return false
	}
	// parts[0] = "" (before first $), parts[1]="argon2id", parts[2]="v=19", parts[3]="m=M,t=T,p=P", parts[4]=salt, parts[5]=hash
	if len(parts) != 6 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	actualHash := argon2.IDKey([]byte(password), salt, t, m, uint8(p), uint32(len(expectedHash)))
	if len(actualHash) != len(expectedHash) {
		return false
	}
	// constant-time comparison
	var diff byte
	for i := range actualHash {
		diff |= actualHash[i] ^ expectedHash[i]
	}
	return diff == 0
}

func splitDollar(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '$' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// GenerateSessionToken returns a (rawToken, sessionID) pair.
// rawToken is sent to the client as a cookie; sessionID is the SHA-256 hex stored in DB.
func GenerateSessionToken() (rawToken, sessionID string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	rawToken = base64.URLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(rawToken))
	sessionID = hex.EncodeToString(sum[:])
	return rawToken, sessionID, nil
}

// SessionIDFromRaw derives the session ID (DB key) from the raw cookie token.
func SessionIDFromRaw(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

// GenerateSetupToken returns a random URL-safe token for first-run setup.
func GenerateSetupToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
