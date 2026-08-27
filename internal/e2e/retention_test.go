//go:build integration

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	mastermetrics "github.com/heckertobias/orkestra/internal/master/metrics"
	"github.com/heckertobias/orkestra/internal/master/retention"
	"github.com/heckertobias/orkestra/internal/master/store"
)

// retentionDB opens the throwaway Postgres used by the integration suite, skipping when none
// is configured.
func retentionDB(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("ORKESTRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ORKESTRA_TEST_DATABASE_URL to a throwaway Postgres to run this test")
	}
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return db
}

// setRetentionConfig writes the admin-set windows into server_config, the values the janitor
// reads on every sweep.
func setRetentionConfig(ctx context.Context, t *testing.T, db *pgxpool.Pool, eventsDays, auditDays int32) {
	t.Helper()
	_, err := store.New(db).UpsertServerConfig(ctx, store.UpsertServerConfigParams{
		PublicUrl:           "",
		EventsRetentionDays: eventsDays,
		AuditRetentionDays:  auditDays,
		UpdatedAt:           time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("upsert server config: %v", err)
	}
}

// counterValue reads the current value of a Prometheus counter.
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

func countRows(ctx context.Context, t *testing.T, db *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// TestRetentionSweepPrunesExpiredRows covers the core contract of #78: one sweep removes rows
// past their window from events and sessions, and leaves everything inside the window alone.
func TestRetentionSweepPrunesExpiredRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := retentionDB(ctx, t)
	defer db.Close()

	serverID := "retention-server-" + uniqueSuffix()
	insertTestServer(ctx, t, db, serverID)
	setRetentionConfig(ctx, t, db, 30, 0)

	now := time.Now()
	stale := now.Add(-90 * 24 * time.Hour).UnixMilli()
	fresh := now.Add(-1 * time.Hour).UnixMilli()

	for _, ts := range []int64{stale, fresh} {
		_, err := db.Exec(ctx, `
			INSERT INTO events (ts, server_id, event_type, severity, message)
			VALUES ($1, $2, 'agent', 'info', 'retention test')`, ts, serverID)
		if err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}

	userID := "retention-user-" + uniqueSuffix()
	_, err := db.Exec(ctx, `
		INSERT INTO users (id, username, created_at) VALUES ($1, $1, $2)`,
		userID, now.UnixMilli())
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	expiredSession := "expired-" + uniqueSuffix()
	liveSession := "live-" + uniqueSuffix()
	for id, expiry := range map[string]int64{
		expiredSession: now.Add(-24 * time.Hour).UnixMilli(),
		liveSession:    now.Add(24 * time.Hour).UnixMilli(),
	} {
		_, err := db.Exec(ctx, `
			INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen)
			VALUES ($1, $2, $3, $4, $3)`, id, userID, now.UnixMilli(), expiry)
		if err != nil {
			t.Fatalf("insert session: %v", err)
		}
	}

	retention.New(db, time.Hour, 30, 0).Sweep(ctx)

	if n := countRows(ctx, t, db,
		`SELECT COUNT(*) FROM events WHERE server_id = $1 AND ts = $2`, serverID, stale); n != 0 {
		t.Errorf("stale event survived the sweep: %d rows left", n)
	}
	if n := countRows(ctx, t, db,
		`SELECT COUNT(*) FROM events WHERE server_id = $1 AND ts = $2`, serverID, fresh); n != 1 {
		t.Errorf("event inside the retention window was deleted: %d rows left, want 1", n)
	}
	if n := countRows(ctx, t, db,
		`SELECT COUNT(*) FROM sessions WHERE id = $1`, expiredSession); n != 0 {
		t.Errorf("expired session survived the sweep: %d rows left", n)
	}
	if n := countRows(ctx, t, db,
		`SELECT COUNT(*) FROM sessions WHERE id = $1`, liveSession); n != 1 {
		t.Errorf("live session was deleted: %d rows left, want 1", n)
	}
}

// TestRetentionKeepsAuditLogByDefault is the regression test for the locked decision that the
// audit trail is opt-in: with the default window of 0 the janitor must not touch audit_log,
// even while it prunes events in the same sweep.
func TestRetentionKeepsAuditLogByDefault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := retentionDB(ctx, t)
	defer db.Close()

	serverID := "retention-audit-server-" + uniqueSuffix()
	insertTestServer(ctx, t, db, serverID)
	setRetentionConfig(ctx, t, db, 30, 0)

	stale := time.Now().Add(-365 * 24 * time.Hour).UnixMilli()
	action := "retention.test." + uniqueSuffix()
	if _, err := db.Exec(ctx, `
		INSERT INTO audit_log (ts, action, target_type) VALUES ($1, $2, 'test')`,
		stale, action); err != nil {
		t.Fatalf("insert audit entry: %v", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO events (ts, server_id, event_type, severity, message)
		VALUES ($1, $2, 'agent', 'info', 'retention test')`, stale, serverID); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	retention.New(db, time.Hour, 30, 0).Sweep(ctx)

	if n := countRows(ctx, t, db, `SELECT COUNT(*) FROM audit_log WHERE action = $1`, action); n != 1 {
		t.Errorf("audit entry was deleted with retention disabled: %d rows left, want 1", n)
	}
	if n := countRows(ctx, t, db,
		`SELECT COUNT(*) FROM events WHERE server_id = $1`, serverID); n != 0 {
		t.Errorf("stale event survived while audit retention was off: %d rows left", n)
	}

	// With a window configured, the same entry is removed — the opt-in actually works.
	setRetentionConfig(ctx, t, db, 30, 30)
	retention.New(db, time.Hour, 30, 0).Sweep(ctx)

	if n := countRows(ctx, t, db, `SELECT COUNT(*) FROM audit_log WHERE action = $1`, action); n != 0 {
		t.Errorf("audit entry survived an explicit retention window: %d rows left", n)
	}
}

// TestRetentionDrainsMultipleBatches verifies the batch loop: a backlog larger than one batch
// is fully drained within a single sweep, and the deletions are counted in the metric.
func TestRetentionDrainsMultipleBatches(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	db := retentionDB(ctx, t)
	defer db.Close()

	serverID := "retention-batch-server-" + uniqueSuffix()
	insertTestServer(ctx, t, db, serverID)
	setRetentionConfig(ctx, t, db, 30, 0)

	const rows = 12000
	stale := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	if _, err := db.Exec(ctx, `
		INSERT INTO events (ts, server_id, event_type, severity, message)
		SELECT $1, $2, 'agent', 'info', 'retention batch test'
		FROM generate_series(1, $3)`, stale, serverID, rows); err != nil {
		t.Fatalf("seed events: %v", err)
	}

	counter := mastermetrics.RetentionDeletedTotal.WithLabelValues("events")
	before := counterValue(t, counter)
	retention.New(db, time.Hour, 30, 0).Sweep(ctx)
	after := counterValue(t, counter)

	if n := countRows(ctx, t, db,
		`SELECT COUNT(*) FROM events WHERE server_id = $1`, serverID); n != 0 {
		t.Errorf("backlog not drained in one sweep: %d rows left", n)
	}
	if deleted := after - before; deleted < rows {
		t.Errorf("deleted-rows metric = %v, want at least %d", deleted, rows)
	}
}
