package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/sessions"
	"github.com/enfec/agentmesh/protocols/agentapi"
)

func (s *Server) createExitSession(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermSessionsExitNode)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		ClientLabel string `json:"client_label"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(w, r, &in); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	if in.ClientLabel == "" {
		in.ClientLabel = r.UserAgent()
	}
	relayURL := strings.TrimRight(s.GatewayURL, "/") + agentapi.PathRelay
	c, err := s.RemoteSessions.CreateExitSession(r.Context(), s.actor(r, p), id, in.ClientLabel, relayURL)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, c)
	return nil
}

func (s *Server) listExitSessions(w http.ResponseWriter, r *http.Request) error {
	p, err := s.requireAny(r, auth.PermSessionsExitNode, auth.PermAuditRead)
	if err != nil {
		return err
	}
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return err
	}
	before, err := cursorUUID(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	v := httpx.Validation{}
	f := sessions.Filter{DeviceID: parseOptUUID(v, "device_id", q.Get("device_id")), ActiveOnly: sessions.ParseActive(q.Get("active"))}
	if err := v.Err(); err != nil {
		return err
	}
	// Without audit.read a user only sees their own sessions.
	if !p.Can(auth.PermAuditRead) {
		uid := p.UserID
		f.UserID = &uid
	}
	items, next, err := s.RemoteSessions.List(r.Context(), p.OrgID, f, before, limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[sessions.Session]{Items: items, NextCursor: uuidCursor(next)})
	return nil
}

func (s *Server) terminateExitSession(w http.ResponseWriter, r *http.Request) error {
	p, err := s.requireAny(r, auth.PermSessionsExitNode, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	sess, err := s.RemoteSessions.Terminate(r.Context(), s.actor(r, p), id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, sess)
	return nil
}
