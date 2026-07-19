// Package telemetry bridges Docker log/stats streams to the Agent gRPC protocol.
package telemetry

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"time"

	dockerevents "github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"github.com/heckertobias/orkestra/internal/agent/dockerctl"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// LogStreamer streams Docker logs as LogChunk messages to a send function.
type LogStreamer struct {
	dc *dockerctl.Client
}

// NewLogStreamer creates a LogStreamer backed by the given dockerctl client.
func NewLogStreamer(dc *dockerctl.Client) *LogStreamer {
	return &LogStreamer{dc: dc}
}

// Stream reads logs from containerID and calls send for each chunk until ctx is cancelled.
func (ls *LogStreamer) Stream(ctx context.Context, streamID, containerID string, follow bool, tail int32, send func(*orkestraV1.LogChunk) error) error {
	tailStr := "all"
	if tail > 0 {
		tailStr = fmt.Sprintf("%d", tail)
	}

	rc, err := ls.dc.Logs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tailStr,
		Timestamps: true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	// Docker multiplexes stdout/stderr with an 8-byte header: [stream_type(1), 0,0,0, size(4)]
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(rc, hdr); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		data := make([]byte, size)
		if _, err := io.ReadFull(rc, data); err != nil {
			return err
		}
		if err := send(&orkestraV1.LogChunk{
			StreamId: streamID,
			Data:     data,
		}); err != nil {
			return err
		}
	}
}

// EventStreamer streams Docker events as DockerEvent messages.
type EventStreamer struct {
	dc *dockerctl.Client
}

// NewEventStreamer creates an EventStreamer.
func NewEventStreamer(dc *dockerctl.Client) *EventStreamer {
	return &EventStreamer{dc: dc}
}

// Stream forwards Docker events until ctx is cancelled.
func (es *EventStreamer) Stream(ctx context.Context, send func(*orkestraV1.DockerEvent) error) error {
	events, errs := es.dc.Events(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			return err
		case ev := <-events:
			var evType string
			switch ev.Type {
			case dockerevents.ContainerEventType:
				evType = "container"
			default:
				evType = string(ev.Type)
			}
			if err := send(&orkestraV1.DockerEvent{
				Type:        evType,
				Action:      string(ev.Action),
				ActorId:     ev.Actor.ID,
				Attributes:  ev.Actor.Attributes,
				TimestampMs: ev.TimeNano / 1e6,
			}); err != nil {
				return err
			}
		}
	}
}

// StatsStreamer polls container stats and sends StatsChunk messages.
type StatsStreamer struct {
	dc *dockerctl.Client
}

// NewStatsStreamer creates a StatsStreamer.
func NewStatsStreamer(dc *dockerctl.Client) *StatsStreamer {
	return &StatsStreamer{dc: dc}
}

// Stream sends a StatsChunk every 5 seconds for all managed containers.
func (ss *StatsStreamer) Stream(ctx context.Context, streamID string, containerIDs []string, send func(*orkestraV1.StatsChunk) error) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			entries, err := ss.collectStats(ctx, containerIDs)
			if err != nil {
				slog.Warn("collect stats error", "err", err)
				continue
			}
			if err := send(&orkestraV1.StatsChunk{
				StreamId:   streamID,
				Containers: entries,
			}); err != nil {
				return err
			}
		}
	}
}

func (ss *StatsStreamer) collectStats(ctx context.Context, containerIDs []string) ([]*orkestraV1.ContainerStats, error) {
	if len(containerIDs) == 0 {
		list, err := ss.dc.ListContainers(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			containerIDs = append(containerIDs, c.ID)
		}
	}

	var out []*orkestraV1.ContainerStats
	for _, id := range containerIDs {
		stat, err := collectOneStat(ctx, ss.dc, id)
		if err != nil {
			continue
		}
		out = append(out, stat)
	}
	return out, nil
}
