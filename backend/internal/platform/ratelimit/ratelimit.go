// Package ratelimit provides an in-memory keyed token-bucket limiter.
//
// It is per-process: with N replicas the effective limit is N times higher.
// That is acceptable for abuse protection in the MVP; a shared Redis-backed
// limiter replaces it in Phase 2.
package ratelimit

import (
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type entry struct {
	lim  *rate.Limiter
	seen time.Time
}

// Keyed limits events per key (IP, email, device id...).
type Keyed struct {
	mu    sync.Mutex
	m     map[string]*entry
	limit rate.Limit
	burst int
	stop  chan struct{}
}

// New creates a limiter allowing `perMinute` events per minute with `burst`.
func New(perMinute float64, burst int) *Keyed {
	k := &Keyed{
		m:     make(map[string]*entry),
		limit: rate.Limit(perMinute / 60),
		burst: burst,
		stop:  make(chan struct{}),
	}
	go k.gc()
	return k
}

// Allow consumes one token for key. When denied it returns the seconds to wait.
func (k *Keyed) Allow(key string) (bool, int) {
	k.mu.Lock()
	e, ok := k.m[key]
	if !ok {
		e = &entry{lim: rate.NewLimiter(k.limit, k.burst)}
		k.m[key] = e
	}
	e.seen = time.Now()
	k.mu.Unlock()

	r := e.lim.Reserve()
	if d := r.Delay(); d > 0 {
		r.Cancel()
		return false, int(math.Ceil(d.Seconds()))
	}
	return true, 0
}

// Close stops the background garbage collector.
func (k *Keyed) Close() { close(k.stop) }

func (k *Keyed) gc() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-k.stop:
			return
		case now := <-t.C:
			k.mu.Lock()
			for key, e := range k.m {
				if now.Sub(e.seen) > 10*time.Minute {
					delete(k.m, key)
				}
			}
			k.mu.Unlock()
		}
	}
}
