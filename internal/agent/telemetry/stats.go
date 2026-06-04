package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"

	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

func collectOneStat(_ context.Context, _ interface{}, id string) (*orkestraV1.ContainerStats, error) {
	// Stub stats — full implementation in M2 integration pass.
	return &orkestraV1.ContainerStats{
		ContainerId: id,
		ServiceName: id[:min(12, len(id))],
	}, nil
}

// dockerStatsFromReader decodes a single Docker stats JSON blob.
func dockerStatsFromReader(r io.Reader) (*container.StatsResponse, error) {
	var stats container.StatsResponse
	if err := json.NewDecoder(r).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode stats: %w", err)
	}
	return &stats, nil
}

// cpuPercent computes the CPU usage percentage from two consecutive stat readings.
func cpuPercent(s *container.StatsResponse) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) -
		float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemUsage) -
		float64(s.PreCPUStats.SystemUsage)
	numCPUs := float64(s.CPUStats.OnlineCPUs)
	if numCPUs == 0 {
		numCPUs = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if systemDelta > 0 && cpuDelta > 0 {
		return (cpuDelta / systemDelta) * numCPUs * 100.0
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
