package api

import (
	"encoding/json"
	"fmt"

	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

func serverFromRow(row store.Server, status string) *orkestraV1.Server {
	var labels map[string]string
	if row.Labels != nil {
		_ = json.Unmarshal(row.Labels, &labels)
	}
	if labels == nil {
		labels = make(map[string]string)
	}

	var lastSeen int64
	if row.LastSeenAt != nil {
		lastSeen = *row.LastSeenAt
	}
	var agentVersion, dockerVersion string
	if row.AgentVersion != nil {
		agentVersion = *row.AgentVersion
	}
	if row.DockerVersion != nil {
		dockerVersion = *row.DockerVersion
	}

	return &orkestraV1.Server{
		Id:            row.ID,
		Name:          row.Name,
		Hostname:      row.Hostname,
		Arch:          row.Arch,
		Os:            row.Os,
		AgentVersion:  agentVersion,
		DockerVersion: dockerVersion,
		Labels:        labels,
		Status:        status,
		LastSeenAt:    lastSeen,
		EnrolledAt:    row.EnrolledAt,
	}
}

func labelsToJSON(labels map[string]string) ([]byte, error) {
	if labels == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}
	return b, nil
}

func ptrInt64(v int64) *int64 { return &v }

func commandTypeFromString(s string) orkestraV1.CommandType {
	switch s {
	case "start":
		return orkestraV1.CommandType_COMMAND_TYPE_START
	case "stop":
		return orkestraV1.CommandType_COMMAND_TYPE_STOP
	case "restart":
		return orkestraV1.CommandType_COMMAND_TYPE_RESTART
	case "pull":
		return orkestraV1.CommandType_COMMAND_TYPE_PULL
	case "remove":
		return orkestraV1.CommandType_COMMAND_TYPE_REMOVE
	case "exec":
		return orkestraV1.CommandType_COMMAND_TYPE_EXEC
	case "prune":
		return orkestraV1.CommandType_COMMAND_TYPE_PRUNE
	default:
		return orkestraV1.CommandType_COMMAND_TYPE_UNSPECIFIED
	}
}
