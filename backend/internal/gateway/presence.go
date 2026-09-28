package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	presenceFlushInterval = 10 * time.Second
	heartbeatSampleEvery  = 5 * time.Minute
)

type touch struct {
	deviceID uuid.UUID
	at       time.Time
	ip       string
}

// presence batches last-seen updates so heartbeat volume does not translate
// into one UPDATE per heartbeat (≈3.3k/s at 100k devices).
type presence struct {
	pool  *pgxpool.Pool
	log   *slog.Logger
	mu    sync.Mutex
	dirty map[uuid.UUID]touch // by session id
}

func newPresence(pool *pgxpool.Pool, log *slog.Logger) *presence {
	return &presence{pool: pool, log: log, dirty: make(map[uuid.UUID]touch)}
}

func (p *presence) touch(sessionID, deviceID uuid.UUID, ip string) {
	p.mu.Lock()
	p.dirty[sessionID] = touch{deviceID: deviceID, at: time.Now().UTC(), ip: ip}
	p.mu.Unlock()
}

func (p *presence) run(ctx context.Context) {
	t := time.NewTicker(presenceFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			p.flush(fctx)
			cancel()
			return
		case <-t.C:
			p.flush(ctx)
		}
	}
}

func (p *presence) flush(ctx context.Context) {
	p.mu.Lock()
	batch := p.dirty
	p.dirty = make(map[uuid.UUID]touch, len(batch))
	p.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	sids := make([]uuid.UUID, 0, len(batch))
	dids := make([]uuid.UUID, 0, len(batch))
	ats := make([]time.Time, 0, len(batch))
	ips := make([]string, 0, len(batch))
	for sid, t := range batch {
		sids = append(sids, sid)
		dids = append(dids, t.deviceID)
		ats = append(ats, t.at)
		ips = append(ips, t.ip)
	}
	if _, err := p.pool.Exec(ctx, `
		UPDATE device_sessions s SET last_seen_at = v.at
		FROM unnest($1::uuid[], $2::timestamptz[]) AS v(id, at)
		WHERE s.id = v.id AND s.disconnected_at IS NULL`, sids, ats); err != nil {
		p.log.Error("presence flush (sessions)", "err", err, "n", len(sids))
	}
	if _, err := p.pool.Exec(ctx, `
		UPDATE devices d SET last_seen_at = v.at, last_ip = v.ip
		FROM unnest($1::uuid[], $2::timestamptz[], $3::text[]) AS v(id, at, ip)
		WHERE d.id = v.id AND (d.last_seen_at IS NULL OR d.last_seen_at < v.at)`, dids, ats, ips); err != nil {
		p.log.Error("presence flush (devices)", "err", err, "n", len(dids))
	}
}
