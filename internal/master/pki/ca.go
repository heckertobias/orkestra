// Package pki manages the internal CA: generation, KEK-encrypted storage, and CSR signing.
package pki

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/chacha20poly1305"
)

// CA holds the in-memory CA certificate and private key.
type CA struct {
	cert    *x509.Certificate
	certPEM string
	key     *ecdsa.PrivateKey
}

// CertPEM returns the CA certificate in PEM format (safe to share with Agents).
func (ca *CA) CertPEM() string { return ca.certPEM }

// TLSCert returns the CA as a tls.Certificate for use in TLS configs.
func (ca *CA) TLSCert() tls.Certificate {
	return tls.Certificate{
		Certificate: [][]byte{ca.cert.Raw},
		PrivateKey:  ca.key,
		Leaf:        ca.cert,
	}
}

// CertPool returns an x509.CertPool containing this CA for client verification.
func (ca *CA) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	return pool
}

// LoadOrCreate loads the CA from the database, or generates a new one on first start.
func LoadOrCreate(ctx context.Context, db *pgxpool.Pool, kek []byte) (*CA, error) {
	var certPEM string
	var keyEnc []byte
	err := db.QueryRow(ctx, `SELECT cert_pem, key_enc FROM ca WHERE id = 1`).
		Scan(&certPEM, &keyEnc)
	if err == nil {
		return loadCA(certPEM, keyEnc, kek)
	}

	// First start — generate CA and persist it.
	ca, certPEMNew, keyEncNew, err := generateCA(kek)
	if err != nil {
		return nil, fmt.Errorf("generate CA: %w", err)
	}
	_, err = db.Exec(ctx,
		`INSERT INTO ca (id, cert_pem, key_enc, created_at) VALUES (1, $1, $2, $3)`,
		certPEMNew, keyEncNew, time.Now().UnixMilli(),
	)
	if err != nil {
		return nil, fmt.Errorf("persist CA: %w", err)
	}
	return ca, nil
}

// SignCSR signs a PKCS#10 CSR and returns a PEM-encoded client certificate, preserving the
// subject from the CSR.
func (ca *CA) SignCSR(csrPEM string, ttl time.Duration) (certPEM string, serial string, err error) {
	return ca.signCSR(csrPEM, "", ttl)
}

// SignCSRWithCN signs a CSR but forces the certificate's Common Name to cn, ignoring whatever
// subject the requester put in the CSR. Only the CSR's public key is trusted — the identity
// (CN) is assigned by the master. This keeps the client-cert CN, which every agent RPC uses
// to identify the agent, in lockstep with the master-assigned agent ID.
func (ca *CA) SignCSRWithCN(csrPEM, cn string, ttl time.Duration) (certPEM string, serial string, err error) {
	return ca.signCSR(csrPEM, cn, ttl)
}

func (ca *CA) signCSR(csrPEM, overrideCN string, ttl time.Duration) (certPEM string, serial string, err error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return "", "", fmt.Errorf("invalid CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return "", "", fmt.Errorf("CSR signature invalid: %w", err)
	}

	subject := csr.Subject
	if overrideCN != "" {
		subject = pkix.Name{CommonName: overrideCN}
	}

	serialNum, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serialNum,
		Subject:      subject,
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(ttl),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, csr.PublicKey, ca.key)
	if err != nil {
		return "", "", fmt.Errorf("sign cert: %w", err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	return certPEM, serialNum.Text(16), nil
}

// IssueServerCert generates a fresh leaf TLS server certificate signed by the CA, valid for
// the given DNS names and IP addresses (SANs). It is used for the Agent gRPC listener so
// agents — which pin this CA as their root — can verify the master's hostname. The leaf key
// is generated in-memory and never persisted; a new leaf is minted on each master start
// (agents trust the CA, not the leaf, so leaf rotation is transparent).
func (ca *CA) IssueServerCert(dnsNames []string, ips []net.IP) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate server key: %w", err)
	}
	serialNum, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serialNum,
		Subject:      pkix.Name{CommonName: "orkestra-master"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("sign server cert: %w", err)
	}
	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse server cert: %w", err)
	}
	// Present the leaf plus the CA cert so agents can build the chain to their pinned CA.
	return tls.Certificate{
		Certificate: [][]byte{certDER, ca.cert.Raw},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}

// CertFingerprint returns the hex SHA-256 fingerprint of a PEM-encoded certificate.
func CertFingerprint(certPEM string) (string, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return "", fmt.Errorf("invalid cert PEM")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

// ParseCertPEM parses a single PEM-encoded certificate.
func ParseCertPEM(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, fmt.Errorf("invalid cert PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

// Encrypt encrypts plaintext with the KEK using XChaCha20-Poly1305.
// Output format: nonce (24 bytes) || ciphertext.
func Encrypt(kek, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext produced by Encrypt.
func Decrypt(kek, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(kek)
	if err != nil {
		return nil, err
	}
	ns := aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

func generateCA(kek []byte) (*CA, string, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, "", nil, err
	}
	serialNum, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serialNum,
		Subject:               pkix.Name{CommonName: "orkestra-ca"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, "", nil, err
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, "", nil, err
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, "", nil, err
	}
	keyEnc, err := Encrypt(kek, keyDER)
	if err != nil {
		return nil, "", nil, err
	}
	return &CA{cert: cert, certPEM: certPEM, key: key}, certPEM, keyEnc, nil
}

func loadCA(certPEM string, keyEnc, kek []byte) (*CA, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, fmt.Errorf("invalid CA cert PEM in database")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}
	keyDER, err := Decrypt(kek, keyEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt CA key (wrong KEK?): %w", err)
	}
	key, err := x509.ParseECPrivateKey(keyDER)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	return &CA{cert: cert, certPEM: certPEM, key: key}, nil
}
