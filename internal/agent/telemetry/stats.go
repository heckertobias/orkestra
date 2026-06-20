package telemetry

import (
	"context"

	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

func collectOneStat(_ context.Context, _ interface{}, id string) (*orkestraV1.ContainerStats, error) {
	// Stub stats — full implementation in M2 integration pass.
	return &orkestraV1.ContainerStats{
		ContainerId: id,
		ServiceName: id[:min(12, len(id))],
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
