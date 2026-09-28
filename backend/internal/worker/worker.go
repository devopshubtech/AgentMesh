// Package worker runs periodic maintenance jobs. Each job takes a Postgres
// advisory lock so that running several worker replicas is safe: exactly one
// replica executes a given job at a time.
package worker

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
)

var (
	jobRuns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentmesh_worker_job_runs_total", Help: "Worker job executions."}, []string{"job", "result"})
	jobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "agentmesh_worker_job_duration_seconds", Help: "Worker job duration."}, []string{"job"})
)

// Config tunes the jobs.
type Config struct {
	OfflineAfter       time.Duration
	HeartbeatRetention time.Duration
}

// Worker schedules the jobs.
type Worker struct {
	pool *pgxpool.Pool
	bus  *bus.Bus
	log  *slog.Logger
	cfg  Config
}

// New constructs a Worker.
func New(pool *pgxpool.Pool, b *bus.Bus, log *slog.Logger, cfg Config) *Worker {
	return &Worker{pool: pool, bus: b, log: log, cfg: cfg}
}

type job struct {
	name  string
	every time.Duration
	fn    func(context.Context) error
}

// Run blocks until ctx is canceled.
func (w *Worker) Run(ctx context.Context) {
	jobs := []job{
		{"reap_offline_devices", 10 * time.Second, w.reapOffline},
		{"expire_commands", 15 * time.Second, w.expireCommands},
		{"gc_auth", 5 * time.Minute, w.gcAuth},
		{"heartbeat_partitions", time.Hour, w.managePartitions},
		{"verify_audit_chain", 6 * time.Hour, w.verifyAudit},
	}
	done := make(chan struct{})
	for _, j := range jobs {
		go func(j job) {
			w.execute(ctx, j) // run once at start
			t := time.NewTicker(j.every)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					done <- struct{}{}
					return
				case <-t.C:
					w.execute(ctx, j)
				}
			}
		}(j)
	}
	for range jobs {
		<-done
	}
}

func lockKey(name string) int64 {
	h := fnv.New64a()
	h.Write([]byte("agentmesh-job:" + name))
	return int64(h.Sum64())
}

func (w *Worker) execute(ctx context.Context, j job) {
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	var got bool
	key := lockKey(j.name)
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&got); err != nil || !got {
		return
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key) }()

	start := time.Now()
	jctx, cancel := context.WithTimeout(ctx, j.every*4)
	defer cancel()
	err = j.fn(jctx)
	jobDuration.WithLabelValues(j.name).Observe(time.Since(start).Seconds())
	if err != nil {
		jobRuns.WithLabelValues(j.name, "error").Inc()
		w.log.Error("job failed", "job", j.name, "err", err)
		return
	}
	jobRuns.WithLabelValues(j.name, "ok").Inc()
}

// reapOffline closes sessions whose gateway stopped refreshing them (crashed
// gateway, network partition) and marks devices without a live session offline.
func (w *Worker) reapOffline(ctx context.Context) error {
	secs := w.cfg.OfflineAfter.Seconds()
	if _, err := w.pool.Exec(ctx, `
		UPDATE device_sessions SET disconnected_at = now(), disconnect_reason = 'heartbeat_timeout'
		WHERE disconnected_at IS NULL AND last_seen_at < now() - make_interval(secs => $1)`, secs); err != nil {
		return err
	}
	rows, err := w.pool.Query(ctx, `
		UPDATE devices d SET connectivity = 'offline', updated_at = now()
		WHERE d.connectivity = 'online'
		  AND (d.last_seen_at IS NULL OR d.last_seen_at < now() - interval '15 seconds')
		  AND NOT EXISTS (SELECT 1 FROM device_sessions s WHERE s.device_id = d.id AND s.disconnected_at IS NULL)
		RETURNING d.id, d.org_id, d.status, d.last_seen_at`)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var ev devices.StatusEvent
		var org uuid.UUID
		if err := rows.Scan(&ev.DeviceID, &org, &ev.Status, &ev.LastSeenAt); err != nil {
			return err
		}
		ev.Connectivity = devices.Offline
		w.bus.PublishEvent(org, bus.EventDeviceStatus, ev)
		n++
	}
	if n > 0 {
		w.log.Info("devices marked offline", "count", n)
	}
	return rows.Err()
}

// expireCommands finalizes commands that can no longer complete.
func (w *Worker) expireCommands(ctx context.Context) error {
	publish := func(sql string) error {
		rows, err := w.pool.Query(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, dev, org uuid.UUID
			var status string
			if err := rows.Scan(&id, &dev, &org, &status); err != nil {
				return err
			}
			w.bus.PublishEvent(org, bus.EventCommandUpdate, map[string]any{"command_id": id, "device_id": dev, "status": status})
		}
		return rows.Err()
	}
	// Never delivered before their deadline.
	if err := publish(`
		UPDATE device_commands SET status = 'expired', finished_at = now()
		WHERE status IN ('queued', 'sent') AND expires_at < now()
		RETURNING id, device_id, org_id, status`); err != nil {
		return err
	}
	// Started but no result long after the timeout (agent crashed/restarted).
	if err := publish(`
		WITH stuck AS (
		    UPDATE device_commands SET status = 'failed', finished_at = now()
		    WHERE status IN ('acked', 'running')
		      AND coalesce(started_at, sent_at, created_at) + make_interval(secs => timeout_s + 120) < now()
		    RETURNING id, device_id, org_id, status)
		, ins AS (
		    INSERT INTO command_results (command_id, error)
		    SELECT id, 'no result received from agent' FROM stuck
		    ON CONFLICT (command_id) DO NOTHING)
		SELECT id, device_id, org_id, status FROM stuck`); err != nil {
		return err
	}
	return nil
}

// gcAuth deletes expired challenges and long-dead user sessions.
func (w *Worker) gcAuth(ctx context.Context) error {
	if _, err := w.pool.Exec(ctx, `DELETE FROM auth_challenges WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx, `DELETE FROM user_sessions WHERE absolute_expires_at < now() - interval '7 days'`)
	return err
}

// managePartitions keeps daily heartbeat partitions ahead of time and drops
// partitions older than the retention window.
func (w *Worker) managePartitions(ctx context.Context) error {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 0; i < 3; i++ {
		day := today.AddDate(0, 0, i)
		name := fmt.Sprintf("device_heartbeats_%s", day.Format("20060102"))
		sql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s PARTITION OF device_heartbeats FOR VALUES FROM ('%s') TO ('%s')`,
			name, day.Format(time.DateOnly), day.AddDate(0, 0, 1).Format(time.DateOnly))
		if _, err := w.pool.Exec(ctx, sql); err != nil {
			// Rows for this range already landed in the default partition;
			// leave them there rather than failing the whole job.
			w.log.Warn("create heartbeat partition", "partition", name, "err", err)
		}
	}
	cutoff := today.Add(-w.cfg.HeartbeatRetention)
	rows, err := w.pool.Query(ctx, `
		SELECT c.relname FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'device_heartbeats' AND c.relname ~ '^device_heartbeats_[0-9]{8}$'`)
	if err != nil {
		return err
	}
	var drop []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		day, err := time.Parse("20060102", name[len("device_heartbeats_"):])
		if err == nil && day.Before(cutoff) {
			drop = append(drop, name)
		}
	}
	rows.Close()
	for _, name := range drop {
		if _, err := w.pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, name)); err != nil {
			return err
		}
		w.log.Info("dropped heartbeat partition", "partition", name)
	}
	_, err = w.pool.Exec(ctx, `DELETE FROM device_heartbeats_default WHERE ts < $1`, cutoff)
	return err
}

// verifyAudit recomputes each org's audit hash chain and alerts on breaks.
func (w *Worker) verifyAudit(ctx context.Context) error {
	rows, err := w.pool.Query(ctx, `SELECT id FROM organizations`)
	if err != nil {
		return err
	}
	var orgs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		orgs = append(orgs, id)
	}
	rows.Close()
	for _, org := range orgs {
		res, err := audit.Verify(ctx, w.pool, org)
		if err != nil {
			return err
		}
		if !res.OK {
			w.log.Error("AUDIT CHAIN BROKEN", "org_id", org, "first_bad_id", *res.FirstBadID, "checked", res.Checked)
		} else {
			w.log.Info("audit chain verified", "org_id", org, "checked", res.Checked)
		}
	}
	return nil
}
