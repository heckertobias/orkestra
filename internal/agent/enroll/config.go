// Package enroll handles one-time Agent enrollment: keypair generation, CSR creation,
// EnrollRPC call, and persistence of the resulting credentials.
package enroll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the persisted Agent configuration written after enrollment.
type Config struct {
	MasterAddr string `json:"master_addr"`
	AgentID    string `json:"agent_id"`
}

// ConfigPath returns the path to the agent config file within dataDir.
func ConfigPath(dataDir string) string { return filepath.Join(dataDir, "config.json") }

// CertPath returns the path to the agent client certificate.
func CertPath(dataDir string) string { return filepath.Join(dataDir, "agent.crt") }

// KeyPath returns the path to the agent private key.
func KeyPath(dataDir string) string { return filepath.Join(dataDir, "agent.key") }

// CAPath returns the path to the CA bundle.
func CAPath(dataDir string) string { return filepath.Join(dataDir, "ca.crt") }

// LoadConfig reads the agent config from dataDir.
func LoadConfig(dataDir string) (*Config, error) {
	data, err := os.ReadFile(ConfigPath(dataDir))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// IsEnrolled returns true if credentials exist in dataDir.
func IsEnrolled(dataDir string) bool {
	for _, p := range []string{ConfigPath(dataDir), CertPath(dataDir), KeyPath(dataDir), CAPath(dataDir)} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

func saveFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}
