// Package conn handles the persistent mTLS gRPC bidi-stream from Agent to Master.
package conn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"

	agentmetrics "github.com/heckertobias/orkestra/internal/agent/metrics"
	"github.com/heckertobias/orkestra/internal/agent/enroll"
	"github.com/heckertobias/orkestra/internal/shared/version"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
	"github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1/orkestrav1connect"
)

const renewThreshold = 30 * 24 * time.Hour

// MessageHandler processes a MasterMessage received from the Master.
type MessageHandler func(ctx context.Context, msg *orkestraV1.MasterMessage) error

// Agent maintains the persistent connection to the Master.
type Agent struct {
	cfg     *enroll.Config
	dataDir string
	handler MessageHandler
}

// New creates an Agent connection manager.
func New(cfg *enroll.Config, dataDir string, handler MessageHandler) *Agent {
	return &Agent{cfg: cfg, dataDir: dataDir, handler: handler}
}

// RunForever connects to the Master and reconnects with exponential backoff on failure.
// It blocks until ctx is cancelled.
func (a *Agent) RunForever(ctx context.Context) {
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := a.checkAndRenewCert(ctx); err != nil {
			slog.Warn("cert renewal check failed", "err", err)
		}
		if err := a.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			agentmetrics.StreamReconnectsTotal.Inc()
			wait := backoff(attempt)
			slog.Warn("agent connection lost, reconnecting",
				"err", err,
				"attempt", attempt+1,
				"wait", wait,
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			attempt++
		} else {
			attempt = 0
		}
	}
}

func (a *Agent) connect(ctx context.Context) error {
	tlsCfg, err := a.mtlsConfig()
	if err != nil {
		return fmt.Errorf("build mTLS config: %w", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
			ForceAttemptHTTP2: true,
		},
	}
	client := orkestrav1connect.NewAgentServiceClient(httpClient, a.cfg.MasterAddr,
		connect.WithGRPC(),
	)

	stream := client.Connect(ctx)

	// Send Hello.
	if err := stream.Send(&orkestraV1.AgentMessage{
		Payload: &orkestraV1.AgentMessage_Hello{
			Hello: &orkestraV1.Hello{
				AgentId:      a.cfg.AgentID,
				AgentVersion: version.Version,
				Hostname:     hostname(),
			},
		},
	}); err != nil {
		return fmt.Errorf("send Hello: %w", err)
	}
	slog.Info("connected to master", "master", a.cfg.MasterAddr, "agent_id", a.cfg.AgentID)

	// Start heartbeat ticker.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Goroutine: send periodic StatusReports.
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := stream.Send(&orkestraV1.AgentMessage{
					Payload: &orkestraV1.AgentMessage_StatusReport{
						StatusReport: &orkestraV1.StatusReport{
							ReportedAtMs: time.Now().UnixMilli(),
						},
					},
				}); err != nil {
					slog.Warn("heartbeat send error", "err", err)
					return
				}
			}
		}
	}()

	// Receive loop.
	for {
		msg, err := stream.Receive()
		if err != nil {
			break
		}
		if a.handler != nil {
			if err := a.handler(ctx, msg); err != nil {
				slog.Error("message handler error", "err", err)
			}
		}
	}

	return stream.CloseResponse()
}

// checkAndRenewCert renews the agent's mTLS cert if it expires within renewThreshold.
func (a *Agent) checkAndRenewCert(ctx context.Context) error {
	certBytes, err := os.ReadFile(enroll.CertPath(a.dataDir))
	if err != nil {
		return fmt.Errorf("read cert: %w", err)
	}
	block, _ := pem.Decode(certBytes)
	if block == nil {
		return fmt.Errorf("decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse cert: %w", err)
	}
	if time.Until(cert.NotAfter) > renewThreshold {
		return nil
	}

	slog.Info("agent cert expiring soon, renewing", "expires_at", cert.NotAfter)

	privKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate renewal keypair: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: a.cfg.AgentID},
	}, privKey)
	if err != nil {
		return fmt.Errorf("create renewal CSR: %w", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	tlsCfg, err := a.mtlsConfig()
	if err != nil {
		return fmt.Errorf("build mTLS config for renewal: %w", err)
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg, ForceAttemptHTTP2: true}}
	client := orkestrav1connect.NewAgentServiceClient(httpClient, a.cfg.MasterAddr, connect.WithGRPC())

	resp, err := client.RenewCert(ctx, connect.NewRequest(&orkestraV1.RenewCertRequest{CsrPem: csrPEM}))
	if err != nil {
		return fmt.Errorf("RenewCert RPC: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("marshal renewal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(enroll.CertPath(a.dataDir), []byte(resp.Msg.ClientCertPem), 0o644); err != nil {
		return fmt.Errorf("save renewed cert: %w", err)
	}
	if err := os.WriteFile(enroll.KeyPath(a.dataDir), keyPEM, 0o600); err != nil {
		return fmt.Errorf("save renewal key: %w", err)
	}
	if resp.Msg.CaBundlePem != "" {
		if err := os.WriteFile(enroll.CAPath(a.dataDir), []byte(resp.Msg.CaBundlePem), 0o644); err != nil {
			return fmt.Errorf("save renewed CA bundle: %w", err)
		}
	}
	slog.Info("agent cert renewed successfully")
	return nil
}

func (a *Agent) mtlsConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(enroll.CertPath(a.dataDir), enroll.KeyPath(a.dataDir))
	if err != nil {
		return nil, fmt.Errorf("load agent cert/key: %w", err)
	}

	caPEM, err := os.ReadFile(enroll.CAPath(a.dataDir))
	if err != nil {
		return nil, fmt.Errorf("read CA bundle: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse CA bundle")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

func backoff(attempt int) time.Duration {
	// Capped exponential: 1s, 2s, 4s, … up to 60s.
	d := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
