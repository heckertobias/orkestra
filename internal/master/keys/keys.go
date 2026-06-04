// Package keys provides the KeySource abstraction for loading the KEK (Key-Encrypting Key).
// The KEK must be held in a separate trust domain from the database credentials.
// See docs/06-security-auth.md § "KEK & KeySource".
package keys

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const keySize = 32 // 256-bit KEK

// KeySource loads the 32-byte KEK at Master startup.
type KeySource interface {
	Load(ctx context.Context) ([]byte, error)
}

// Load selects and runs the appropriate KeySource based on environment variables.
// Priority: ORKESTRA_MASTER_KEY_FILE → ORKESTRA_MASTER_KEY (with warning) → error.
func Load(ctx context.Context) ([]byte, error) {
	if path := os.Getenv("ORKESTRA_MASTER_KEY_FILE"); path != "" {
		slog.Debug("loading KEK from file", "path", path)
		return (&fileSource{path: path}).Load(ctx)
	}
	if raw := os.Getenv("ORKESTRA_MASTER_KEY"); raw != "" {
		slog.Warn("KEK loaded from ORKESTRA_MASTER_KEY env var — " +
			"this is insecure in production; use ORKESTRA_MASTER_KEY_FILE pointing to a secret mount")
		return (&envSource{raw: raw}).Load(ctx)
	}
	return nil, fmt.Errorf("no KEK source configured: set ORKESTRA_MASTER_KEY_FILE " +
		"(recommended) or ORKESTRA_MASTER_KEY (dev only)")
}

// decodeHex parses a hex-encoded 32-byte key, trimming whitespace.
func decodeHex(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(b) != keySize {
		return nil, fmt.Errorf("KEK must be %d bytes (%d hex chars), got %d bytes", keySize, keySize*2, len(b))
	}
	return b, nil
}

// fileSource reads the KEK from a file (Docker/K8s secret mount or chmod-600 file).
type fileSource struct{ path string }

func (f *fileSource) Load(_ context.Context) ([]byte, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return nil, fmt.Errorf("reading KEK file %q: %w", f.path, err)
	}
	return decodeHex(string(data))
}

// envSource reads the KEK from an environment variable (dev/test only).
type envSource struct{ raw string }

func (e *envSource) Load(_ context.Context) ([]byte, error) {
	return decodeHex(e.raw)
}
