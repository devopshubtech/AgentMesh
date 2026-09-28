package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/platform/bus"
)

// events streams org events as Server-Sent Events until the access token
// expires, the client disconnects, or the hub drops a slow client.
func (s *Server) events(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermDevicesRead)
	if err != nil {
		return err
	}
	rc := http.NewResponseController(w)
	// The server-wide write timeout must not cut the stream.
	_ = rc.SetWriteDeadline(time.Time{})

	exp, _ := r.Context().Value(tokenExpiryKey{}).(time.Time)
	if exp.IsZero() {
		exp = time.Now().Add(10 * time.Minute)
	}
	expired := time.NewTimer(time.Until(exp))
	defer expired.Stop()

	c := s.Hub.Subscribe(p.OrgID)
	defer s.Hub.Unsubscribe(c)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, ": connected\nretry: 3000\n\n"); err != nil {
		return nil
	}
	if err := rc.Flush(); err != nil {
		return nil
	}

	canCommands := p.Can(auth.PermCommandsRead)
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-c.Closed:
			return nil
		case <-expired.C:
			_, _ = fmt.Fprint(w, "event: session.expired\ndata: {}\n\n")
			_ = rc.Flush()
			return nil
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
		case ev := <-c.C:
			if (ev.Kind == bus.EventCommandOutput || ev.Kind == bus.EventCommandUpdate) && !canCommands {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, ev.Data); err != nil {
				return nil
			}
		}
		if err := rc.Flush(); err != nil {
			return nil
		}
	}
}
