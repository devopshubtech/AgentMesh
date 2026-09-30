package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/sessions"
	"github.com/enfec/agentmesh/protocols/agentapi"
)

// ConnectKeyHeader carries a connect key on the public session endpoint.
const ConnectKeyHeader = "X-AgentMesh-Connect-Key"

func (s *Server) createConnectKey(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.require(r, auth.PermSessionsExitNode); err != nil {
		return err
	}
	p, err := s.require(r, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Label      string `json:"label"`
		ExpiresInS int    `json:"expires_in_s"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	k, err := s.RemoteSessions.CreateConnectKey(r.Context(), s.actor(r, p), id, in.Label, in.ExpiresInS, s.GatewayURL, s.RendezvousURL)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, k)
	return nil
}

func (s *Server) listConnectKeys(w http.ResponseWriter, r *http.Request) error {
	p, err := s.requireAny(r, auth.PermSessionsExitNode, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	keys, err := s.RemoteSessions.ListConnectKeys(r.Context(), p.OrgID, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[sessions.ConnectKey]{Items: keys})
	return nil
}

func (s *Server) revokeConnectKey(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := s.RemoteSessions.RevokeConnectKey(r.Context(), s.actor(r, p), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// openConnectSession is public: the connect key is the credential.
func (s *Server) openConnectSession(w http.ResponseWriter, r *http.Request) error {
	ip := httpx.ClientIP(r.Context())
	if ok, wait := s.connectRL.Allow(ip); !ok {
		return httpx.RateLimited(wait)
	}
	key := strings.TrimSpace(r.Header.Get(ConnectKeyHeader))
	var in struct {
		ClientLabel string `json:"client_label"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(w, r, &in); err != nil {
			return err
		}
	}
	if in.ClientLabel == "" {
		in.ClientLabel = r.UserAgent()
	}
	relayURL := strings.TrimRight(s.GatewayURL, "/") + agentapi.PathRelay
	c, err := s.RemoteSessions.OpenWithKey(r.Context(), key, in.ClientLabel, ip, httpx.RequestID(r.Context()), relayURL)
	if errors.Is(err, sessions.ErrInvalidConnectKey) {
		return httpx.WithCode(http.StatusUnauthorized, "invalid_connect_key", "this connect link is invalid, expired or was revoked")
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"session_id": c.Session.ID, "device_id": c.Session.DeviceID, "device_name": c.DeviceName,
		"relay_url": c.RelayURL, "ticket": c.Ticket, "ticket_expires_at": c.TicketExpiresAt,
	})
	return nil
}

func (s *Server) createPairCode(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.require(r, auth.PermSessionsExitNode); err != nil {
		return err
	}
	p, err := s.require(r, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Label      string `json:"label"`
		ExpiresInS int    `json:"expires_in_s"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	pc, err := s.RemoteSessions.CreatePairCode(r.Context(), s.actor(r, p), id, in.Label, time.Duration(in.ExpiresInS)*time.Second)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, pc)
	return nil
}

func (s *Server) listPairCodes(w http.ResponseWriter, r *http.Request) error {
	p, err := s.requireAny(r, auth.PermSessionsExitNode, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	codes, err := s.RemoteSessions.ListPairCodes(r.Context(), p.OrgID, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[sessions.PairCode]{Items: codes})
	return nil
}

func (s *Server) revokePairCode(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := s.RemoteSessions.RevokePairCode(r.Context(), s.actor(r, p), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// redeemPairCode is public: the 6-digit code is the credential. Besides the
// per-IP limit, a global limit caps guessing no matter how many addresses an
// attacker uses (X-Forwarded-For is client-controlled behind the tunnel).
func (s *Server) redeemPairCode(w http.ResponseWriter, r *http.Request) error {
	ip := httpx.ClientIP(r.Context())
	if ok, wait := s.connectRL.Allow(ip); !ok {
		return httpx.RateLimited(wait)
	}
	if ok, wait := s.pairRL.Allow("all"); !ok {
		return httpx.RateLimited(wait)
	}
	var in struct {
		Code        string `json:"code"`
		ClientLabel string `json:"client_label"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	p, err := s.RemoteSessions.RedeemPairCode(r.Context(), in.Code, in.ClientLabel, ip, httpx.RequestID(r.Context()), s.GatewayURL, s.RendezvousURL)
	if errors.Is(err, sessions.ErrInvalidPairCode) {
		return httpx.WithCode(http.StatusUnauthorized, "invalid_pair_code", "this code is wrong or has expired")
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"key": p.Key, "link": p.Link, "device_name": p.DeviceName,
		"server_url": strings.TrimRight(s.GatewayURL, "/"), "rendezvous_url": s.RendezvousURL,
	})
	return nil
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermUsersManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Users.Delete(r.Context(), p, id, httpx.RequestID(r.Context()), httpx.ClientIP(r.Context())); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
