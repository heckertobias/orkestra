package enroll

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"runtime"

	"connectrpc.com/connect"

	"github.com/heckertobias/orkestra/internal/shared/version"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
	"github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1/orkestrav1connect"
)

// Params holds the inputs for enrollment.
type Params struct {
	MasterAddr     string
	BootstrapToken string
	ServerName     string // human-readable name
	DataDir        string
}

// Run performs the full enrollment flow and persists credentials to DataDir.
func Run(ctx context.Context, p Params) error {
	// Generate ephemeral ECDSA key pair — private key never leaves the agent.
	privKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate keypair: %w", err)
	}

	// Build CSR with CN = server name (will be overwritten by master with agentID on renewal,
	// but for initial enrollment the CN is informational).
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: p.ServerName},
	}, privKey)
	if err != nil {
		return fmt.Errorf("create CSR: %w", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	// Call Enroll RPC — server TLS only (no client cert yet), so we accept any server cert for now.
	// After enrollment the agent will pin the returned CA bundle.
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // first contact only
		},
	}
	client := orkestrav1connect.NewAgentServiceClient(httpClient, p.MasterAddr,
		connect.WithGRPC(),
	)

	resp, err := client.Enroll(ctx, connect.NewRequest(&orkestraV1.EnrollRequest{
		BootstrapToken: p.BootstrapToken,
		CsrPem:         csrPEM,
		NodeInfo: &orkestraV1.Hello{
			AgentVersion:  version.Version,
			DockerVersion: "",
			Hostname:      p.ServerName,
			Os:            "linux",
			Arch:          runtime.GOARCH,
		},
	}))
	if err != nil {
		return fmt.Errorf("enroll RPC: %w", err)
	}

	msg := resp.Msg
	if msg.AgentId == "" || msg.ClientCertPem == "" || msg.CaBundlePem == "" {
		return fmt.Errorf("incomplete enrollment response from master")
	}

	// After enrollment, update CSR/cert CN to agentID for subsequent mTLS.
	// Re-sign the existing key with the agentID CN isn't needed — the cert was already signed
	// by the CA with whatever CN the master chose. The agentID is in the response.
	// For future RenewCert calls, the CN should be the agentID.

	// Marshal private key.
	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// Persist credentials.
	if err := saveFile(CertPath(p.DataDir), []byte(msg.ClientCertPem), 0o644); err != nil {
		return fmt.Errorf("save cert: %w", err)
	}
	if err := saveFile(KeyPath(p.DataDir), keyPEM, 0o600); err != nil {
		return fmt.Errorf("save key: %w", err)
	}
	if err := saveFile(CAPath(p.DataDir), []byte(msg.CaBundlePem), 0o644); err != nil {
		return fmt.Errorf("save CA bundle: %w", err)
	}

	cfg := &Config{
		MasterAddr: p.MasterAddr,
		AgentID:    msg.AgentId,
	}
	cfgJSON, _ := json.MarshalIndent(cfg, "", "  ")
	if err := saveFile(ConfigPath(p.DataDir), cfgJSON, 0o644); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	return nil
}
