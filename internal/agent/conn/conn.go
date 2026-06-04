// Package conn handles the persistent mTLS gRPC bidi-stream from Agent to Master.
package conn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"

	"github.com/heckertobias/orkestra/internal/agent/enroll"
	"github.com/heckertobias/orkestra/internal/shared/version"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
	"github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1/orkestrav1connect"
)

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
		if err := a.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
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
