package agentgw

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
)

// MTLSMiddleware extracts the Agent ID from the mTLS client certificate CN and injects it
// into the request context for use by the Connect handler.
func MTLSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			agentID := r.TLS.PeerCertificates[0].Subject.CommonName
			if agentID != "" {
				r = r.WithContext(WithAgentID(r.Context(), agentID))
			}
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
