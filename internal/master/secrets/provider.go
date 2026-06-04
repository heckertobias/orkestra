// Package secrets manages secret encryption for the builtin provider.
// Encryption delegates to pki.Encrypt/Decrypt (XChaCha20-Poly1305 + KEK).
package secrets

import (
	"fmt"

	"github.com/heckertobias/orkestra/internal/master/pki"
)

// Seal encrypts plaintext with the KEK and returns the ciphertext blob.
func Seal(kek, plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("empty secret value")
	}
	return pki.Encrypt(kek, plaintext)
}

// Open decrypts a ciphertext blob produced by Seal.
func Open(kek, ciphertext []byte) ([]byte, error) {
	return pki.Decrypt(kek, ciphertext)
}
