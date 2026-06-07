package store

import "context"

const insertEvent = `-- name: InsertEvent :exec
INSERT INTO events (ts, server_id, stack_id, event_type, severity, message, detail_json)
VALUES ($1, $2, $3, $4, $5, $6, $7)
`

type InsertEventParams struct {
	Ts         int64   `json:"ts"`
	ServerID   *string `json:"server_id"`
	StackID    *string `json:"stack_id"`
	EventType  string  `json:"event_type"`
	Severity   string  `json:"severity"`
	Message    string  `json:"message"`
	DetailJson []byte  `json:"detail_json"`
}

func (q *Queries) InsertEvent(ctx context.Context, arg InsertEventParams) error {
	_, err := q.db.Exec(ctx, insertEvent,
		arg.Ts, arg.ServerID, arg.StackID,
		arg.EventType, arg.Severity, arg.Message, arg.DetailJson,
	)
	return err
}

const listRecentEvents = `-- name: ListRecentEvents :many
SELECT id, ts, server_id, stack_id, event_type, severity, message, detail_json
FROM events ORDER BY ts DESC LIMIT $1
`

func (q *Queries) ListRecentEvents(ctx context.Context, limit int32) ([]Event, error) {
	rows, err := q.db.Query(ctx, listRecentEvents, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Event
	for rows.Next() {
		var i Event
		if err := rows.Scan(
			&i.ID, &i.Ts, &i.ServerID, &i.StackID,
			&i.EventType, &i.Severity, &i.Message, &i.DetailJson,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const listEventsAfter = `-- name: ListEventsAfter :many
SELECT id, ts, server_id, stack_id, event_type, severity, message, detail_json
FROM events
WHERE id > $1
  AND ($2::text IS NULL OR server_id = $2)
  AND ($3::text IS NULL OR stack_id = $3)
ORDER BY id ASC LIMIT 50
`

type ListEventsAfterParams struct {
	ID       int64   `json:"id"`
	ServerID *string `json:"server_id"`
	StackID  *string `json:"stack_id"`
}

func (q *Queries) ListEventsAfter(ctx context.Context, arg ListEventsAfterParams) ([]Event, error) {
	rows, err := q.db.Query(ctx, listEventsAfter, arg.ID, arg.ServerID, arg.StackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Event
	for rows.Next() {
		var i Event
		if err := rows.Scan(
			&i.ID, &i.Ts, &i.ServerID, &i.StackID,
			&i.EventType, &i.Severity, &i.Message, &i.DetailJson,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const listEventsFiltered = `-- name: ListEventsFiltered :many
SELECT id, ts, server_id, stack_id, event_type, severity, message, detail_json
FROM events
WHERE ($1::text IS NULL OR server_id = $1)
  AND ($2::text IS NULL OR stack_id = $2)
ORDER BY ts DESC LIMIT 100
`

type ListEventsFilteredParams struct {
	ServerID *string `json:"server_id"`
	StackID  *string `json:"stack_id"`
}

func (q *Queries) ListEventsFiltered(ctx context.Context, arg ListEventsFilteredParams) ([]Event, error) {
	rows, err := q.db.Query(ctx, listEventsFiltered, arg.ServerID, arg.StackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Event
	for rows.Next() {
		var i Event
		if err := rows.Scan(
			&i.ID, &i.Ts, &i.ServerID, &i.StackID,
			&i.EventType, &i.Severity, &i.Message, &i.DetailJson,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
