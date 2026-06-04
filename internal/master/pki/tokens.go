package pki

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateEnrollmentToken generates a new enrollment token, persists the hash, and returns
// the raw token (shown to the operator once — never stored in plaintext).
func CreateEnrollmentToken(ctx context.Context, db *pgxpool.Pool, description string, ttl time.Duration, maxUses int, createdBy *string) (rawToken string, id string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	rawHex := hex.EncodeToString(raw)
	hash := sha256TokenHash(rawHex)

	id = uuid.NewString()
	now := time.Now().UnixMilli()
	expires := time.Now().Add(ttl).UnixMilli()
	maxU := int64(maxUses)

	_, err = db.Exec(ctx, `
		INSERT INTO enrollment_tokens
		  (id, token_hash, description, ttl_seconds, max_uses, created_by, created_at, expires_at, revoked)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE)`,
		id, hash, description, int64(ttl.Seconds()), maxU, createdBy, now, expires,
	)
	if err != nil {
		return "", "", fmt.Errorf("persist enrollment token: %w", err)
	}
	return rawHex, id, nil
}

// ValidateEnrollmentToken checks the token hash, expiry, use count, and revocation.
// On success it increments used_count and returns the token ID.
func ValidateEnrollmentToken(ctx context.Context, db *pgxpool.Pool, rawToken string) (tokenID string, err error) {
	hash := sha256TokenHash(rawToken)
	now := time.Now().UnixMilli()

	var (
		id        string
		usedCount int64
		maxUses   int64
		expiresAt int64
		revoked   bool
	)
	err = db.QueryRow(ctx, `
		SELECT id, used_count, max_uses, expires_at, revoked
		FROM enrollment_tokens WHERE token_hash = $1`, hash).
		Scan(&id, &usedCount, &maxUses, &expiresAt, &revoked)
	if err != nil {
		return "", fmt.Errorf("token not found")
	}
	if revoked {
		return "", fmt.Errorf("token has been revoked")
	}
	if now > expiresAt {
		return "", fmt.Errorf("token has expired")
	}
	if usedCount >= maxUses {
		return "", fmt.Errorf("token max uses reached")
	}

	_, err = db.Exec(ctx,
		`UPDATE enrollment_tokens SET used_count = used_count + 1 WHERE id = $1`, id)
	if err != nil {
		return "", fmt.Errorf("increment token usage: %w", err)
	}
	return id, nil
}

func sha256TokenHash(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
