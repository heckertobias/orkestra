package compose

import (
	"context"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/cli"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// loadFromBytes loads a compose-go Project from in-memory YAML bytes.
func loadFromBytes(data []byte, projectName string, env map[string]string) (*composetypes.Project, error) {
	// Write to a temp file — compose-go's loader needs a file path.
	tmp, err := os.MkdirTemp("", "orkestra-compose-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	composeFile := filepath.Join(tmp, "compose.yaml")
	if err := os.WriteFile(composeFile, data, 0o600); err != nil {
		return nil, err
	}

	// Build an environment slice for variable substitution.
	environ := make([]string, 0, len(env))
	for k, v := range env {
		environ = append(environ, k+"="+v)
	}

	opts, err := cli.NewProjectOptions(
		[]string{composeFile},
		cli.WithName(projectName),
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithConfigFileEnv,
	)
	if err != nil {
		return nil, err
	}

	return opts.LoadProject(context.Background())
}
