package agentgw

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"

	"github.com/heckertobias/orkestra/internal/master/pki"
)

// RevocationChecker reports whether a certificate fingerprint has been revoked.
// Returning (true, nil) causes the connection to be rejected with 403.
type RevocationChecker func(ctx context.Context, fingerprint string) (revoked bool, err error)

// MTLSMiddleware extracts the Agent ID from the mTLS client certificate CN and injects it
// into the request context. If checker is non-nil, revoked certs are rejected with 403.
func MTLSMiddleware(checker RevocationChecker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		clientCert := r.TLS.PeerCertificates[0]
		agentID := clientCert.Subject.CommonName

		if checker != nil && agentID != "" {
			certPEM := string(pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE",
				Bytes: clientCert.Raw,
			}))
			fp, err := pki.CertFingerprint(certPEM)
			if err == nil {
				if revoked, _ := checker(r.Context(), fp); revoked {
					http.Error(w, "certificate revoked", http.StatusForbidden)
					return
				}
			}
		}

		if agentID != "" {
			r = r.WithContext(WithAgentID(r.Context(), agentID))
		}
		next.ServeHTTP(w, r)
	})
}

// NewAgentTLSConfig returns a tls.Config for the Agent gRPC listener (mTLS required).
func NewAgentTLSConfig(serverCert tls.Certificate, clientCA *x509.CertPool) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    clientCA,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h2"},
	}
}
