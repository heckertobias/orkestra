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

// EnrollProcedure is the one Connect/gRPC procedure on AgentService that is reachable
// without a client certificate: enrollment is the bootstrap step where the agent has no
// cert yet and authenticates with a bootstrap token instead. Every other procedure
// requires a verified client cert (enforced fail-closed in MTLSMiddleware).
const EnrollProcedure = "/orkestra.v1.AgentService/Enroll"

// MTLSMiddleware enforces mutual TLS per-RPC (the listener uses VerifyClientCertIfGiven,
// so a missing client cert reaches the application layer rather than being rejected at the
// handshake). It fails closed: every request without a verified client cert is rejected
// with 401 except the one-time Enroll procedure. When a cert is present it is already
// CA-verified by the TLS stack; this middleware then rejects revoked certs (403) and
// injects the Agent ID (from the cert CN) into the request context.
func MTLSMiddleware(checker RevocationChecker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			// No client cert. Only the bootstrap Enroll procedure is allowed through;
			// its own authentication is the bootstrap token. Exact match — never a
			// suffix — so a future procedure ending in "Enroll" cannot become public.
			if r.URL.Path == EnrollProcedure {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "client certificate required", http.StatusUnauthorized)
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

// NewAgentTLSConfig returns a tls.Config for the Agent gRPC listener. It uses
// VerifyClientCertIfGiven (not RequireAndVerifyClientCert) so the one-time Enroll call,
// which has no client cert yet, can complete the handshake; any cert that IS presented is
// still fully verified against clientCA. The per-RPC cert requirement is enforced by
// MTLSMiddleware. This also keeps the listener forward-compatible with sharing a port with
// certificate-less browser traffic (e.g. on 443) in the future.
func NewAgentTLSConfig(serverCert tls.Certificate, clientCA *x509.CertPool) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    clientCA,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h2"},
	}
}
