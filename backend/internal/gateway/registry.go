package gateway

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

var (
	metricConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "agentmesh_gateway_connections", Help: "Active agent WebSocket connections."})
	metricMessagesIn = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentmesh_gateway_messages_in_total", Help: "Messages received from agents."}, []string{"type"})
	metricMessagesOut = promauto.NewCounter(prometheus.CounterOpts{
		Name: "agentmesh_gateway_messages_out_total", Help: "Messages sent to agents."})
	metricRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentmesh_gateway_connections_rejected_total", Help: "Rejected connection attempts."}, []string{"reason"})
)

// registry tracks the sessions held by this gateway replica.
type registry struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*session // by session id
	count    atomic.Int64
	draining atomic.Bool
	wg       sync.WaitGroup
}

func newRegistry() *registry { return &registry{sessions: make(map[uuid.UUID]*session)} }

func (r *registry) add(s *session) {
	r.mu.Lock()
	r.sessions[s.id] = s
	r.mu.Unlock()
	r.count.Add(1)
	metricConnections.Inc()
}

func (r *registry) remove(s *session) {
	r.mu.Lock()
	if _, ok := r.sessions[s.id]; ok {
		delete(r.sessions, s.id)
		r.count.Add(-1)
		metricConnections.Dec()
	}
	r.mu.Unlock()
}

// drainAll asks every agent to reconnect elsewhere after a random delay, so a
// rolling restart does not cause a synchronized reconnect storm.
func (r *registry) drainAll(maxDelayS int) {
	r.draining.Store(true)
	r.mu.Lock()
	list := make([]*session, 0, len(r.sessions))
	for _, s := range r.sessions {
		list = append(list, s)
	}
	r.mu.Unlock()
	for _, s := range list {
		s.disconnect(agentv1.Disconnect_REASON_RECONNECT, "gateway shutting down", uint32(1+rand.IntN(maxDelayS)))
	}
}
