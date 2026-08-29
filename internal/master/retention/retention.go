// Package retention prunes the Master's append-only tables so they stay bounded.
//
// Three tables grow without limit if nobody sweeps them: `events` (one row per agent
// connect/disconnect today, far more once Docker events are forwarded), `audit_log`
// (one row per mutating action), and `sessions` (rows outlive their expiry forever).
// A janitor deletes rows past their retention window in bounded batches.
//
// The window for `events` and `audit_log` is configurable and has three states:
// a negative value inherits the startup default, zero keeps rows forever, and a
// positive value is a number of days. `audit_log` defaults to keeping everything —
// the audit trail is the compliance surface and must not shrink by accident.
// Expired sessions are always swept; `expires_at` already is the rule.
package retention

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mastermetrics "github.com/heckertobias/orkestra/internal/master/metrics"
	"github.com/heckertobias/orkestra/internal/master/store"
)

const (
	// batchSize caps how many rows a single DELETE removes, so the job never holds a
	// long lock on a table the UI reads from.
	batchSize = 5000

	// sweepBudget bounds one sweep across all tables. A large backlog (the first sweep
	// after enabling retention on an old deployment) is drained over several sweeps
	// instead of one long transaction storm.
	sweepBudget = 30 * time.Second

	// KeepForever is the resolved window for "never delete from this table".
	KeepForever time.Duration = 0
)

// deleteFn removes at most batchSize rows older than cutoff and reports how many it
// deleted. It matches the generated store.Delete*Before signatures.
type deleteFn func(ctx context.Context, cutoff int64, limit int32) (int64, error)

// Retention periodically deletes rows past their retention window.
type Retention struct {
	q         *store.Queries
	interval  time.Duration
	envEvents int // startup default in days, used when the DB value is negative
	envAudit  int
}

// New creates a Retention janitor that sweeps every interval. envEvents and envAudit are
// the startup defaults (in days) that apply until an admin sets a window in the UI;
// 0 means "keep forever" and a negative value is clamped to the built-in default.
func New(db *pgxpool.Pool, interval time.Duration, envEvents, envAudit int) *Retention {
	return &Retention{
		q:         store.New(db),
		interval:  interval,
		envEvents: envEvents,
		envAudit:  envAudit,
	}
}

// Run sweeps once immediately and then every interval. Blocks until ctx is done.
// The first sweep is immediate on purpose: a janitor that only starts working after a
// full interval does nothing at all on a Master that restarts more often than that.
func (r *Retention) Run(ctx context.Context) {
	r.Sweep(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Sweep(ctx)
		}
	}
}

// Sweep deletes one round of expired rows from every table. It never writes an `events`
// row about its own work — a retention job that logs into the table it prunes becomes its
// own workload. Progress is reported through slog and Prometheus only.
func (r *Retention) Sweep(ctx context.Context) {
	deadline := time.Now().Add(sweepBudget)
	now := time.Now()
	ok := true

	events, audit := r.windows(ctx)

	if events > 0 {
		if err := r.prune(ctx, "events", now.Add(-events).UnixMilli(), deadline, r.deleteEvents); err != nil {
			slog.Error("retention sweep failed", "table", "events", "err", err)
			ok = false
		}
	}
	if audit > 0 {
		if err := r.prune(ctx, "audit_log", now.Add(-audit).UnixMilli(), deadline, r.deleteAuditLog); err != nil {
			slog.Error("retention sweep failed", "table", "audit_log", "err", err)
			ok = false
		}
	}
	// Sessions have no configurable window: once expires_at has passed, the row cannot
	// authenticate anyone (GetSession filters on it), so it is pure ballast.
	if err := r.prune(ctx, "sessions", now.UnixMilli(), deadline, r.deleteSessions); err != nil {
		slog.Error("retention sweep failed", "table", "sessions", "err", err)
		ok = false
	}

	if ok {
		mastermetrics.RetentionLastSuccess.Set(float64(time.Now().Unix()))
	}
}

// windows resolves the effective retention windows, preferring the admin-set values in
// server_config over the startup defaults. A missing config row means "nothing set yet".
func (r *Retention) windows(ctx context.Context) (events, audit time.Duration) {
	cfg, err := r.q.GetServerConfig(ctx)
	if err != nil {
		// No row is the normal case on a fresh install; anything else is worth knowing about,
		// because it means the sweep is silently ignoring admin-set windows.
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("retention could not read server config, using startup defaults", "err", err)
		}
		return Resolve(-1, r.envEvents), Resolve(-1, r.envAudit)
	}
	return Resolve(int(cfg.EventsRetentionDays), r.envEvents),
		Resolve(int(cfg.AuditRetentionDays), r.envAudit)
}

// Resolve turns a configured retention value into a duration. dbDays wins when it is not
// negative; a negative dbDays inherits envDays. Zero on either side means KeepForever, and
// a negative envDays is treated as unset rather than as "delete everything".
func Resolve(dbDays, envDays int) time.Duration {
	days := dbDays
	if days < 0 {
		days = envDays
	}
	if days <= 0 {
		return KeepForever
	}
	return time.Duration(days) * 24 * time.Hour
}

// prune deletes rows older than cutoff in batches until a batch comes back short (nothing
// left) or the sweep budget is spent. Whatever remains is picked up by the next sweep.
func (r *Retention) prune(ctx context.Context, table string, cutoff int64, deadline time.Time, del deleteFn) error {
	var total int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := del(ctx, cutoff, batchSize)
		if n > 0 {
			total += n
			mastermetrics.RetentionDeletedTotal.WithLabelValues(table).Add(float64(n))
		}
		if err != nil {
			return err
		}
		if n < batchSize {
			break
		}
		if time.Now().After(deadline) {
			slog.Warn("retention sweep hit its time budget, resuming next run",
				"table", table, "deleted", total)
			break
		}
	}
	if total > 0 {
		slog.Info("retention deleted rows", "table", table, "rows", total)
	}
	return nil
}

func (r *Retention) deleteEvents(ctx context.Context, cutoff int64, limit int32) (int64, error) {
	return r.q.DeleteEventsBefore(ctx, store.DeleteEventsBeforeParams{Cutoff: cutoff, BatchSize: limit})
}

func (r *Retention) deleteAuditLog(ctx context.Context, cutoff int64, limit int32) (int64, error) {
	return r.q.DeleteAuditLogBefore(ctx, store.DeleteAuditLogBeforeParams{Cutoff: cutoff, BatchSize: limit})
}

func (r *Retention) deleteSessions(ctx context.Context, cutoff int64, limit int32) (int64, error) {
	return r.q.DeleteExpiredSessions(ctx, store.DeleteExpiredSessionsParams{Cutoff: cutoff, BatchSize: limit})
}
