package agentgw

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestCert returns a minimal self-signed leaf cert with the given CN, for populating
// r.TLS.PeerCertificates in tests (the TLS stack would already have verified it in prod).
func newTestCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

func TestMTLSMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		withCert   bool
		wantStatus int
		wantNext   bool
		wantAgent  string
	}{
		{name: "enroll without cert is allowed", path: EnrollProcedure, withCert: false, wantStatus: http.StatusOK, wantNext: true},
		{name: "non-enroll without cert is rejected", path: "/orkestra.v1.AgentService/Connect", withCert: false, wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "suffix match does not open a public path", path: "/orkestra.v1.AgentService/FooEnroll", withCert: false, wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "enroll with cert still works", path: EnrollProcedure, withCert: true, wantStatus: http.StatusOK, wantNext: true, wantAgent: "agent-42"},
		{name: "non-enroll with cert is allowed and injects agent id", path: "/orkestra.v1.AgentService/Connect", withCert: true, wantStatus: http.StatusOK, wantNext: true, wantAgent: "agent-42"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var nextCalled bool
			var gotAgent string
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				gotAgent, _ = agentIDFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			mw := MTLSMiddleware(nil, next)

			req := httptest.NewRequest(http.MethodPost, "https://master.example"+tc.path, nil)
			if tc.withCert {
				req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestCert(t, "agent-42")}}
			}
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if nextCalled != tc.wantNext {
				t.Errorf("next called = %v, want %v", nextCalled, tc.wantNext)
			}
			if tc.wantNext && gotAgent != tc.wantAgent {
				t.Errorf("agent id = %q, want %q", gotAgent, tc.wantAgent)
			}
		})
	}
}
