package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/commands"
	"github.com/enfec/agentmesh/backend/internal/devices"
	"github.com/enfec/agentmesh/backend/internal/enrollment"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/users"
)

// ---------------------------------------------------------------- devices

var (
	validStatus       = map[string]bool{"": true, devices.StatusPending: true, devices.StatusActive: true, devices.StatusDisabled: true, devices.StatusRevoked: true}
	validConnectivity = map[string]bool{"": true, devices.Online: true, devices.Offline: true}
)

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermDevicesRead)
	if err != nil {
		return err
	}
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	f := devices.Filter{Status: q.Get("status"), Connectivity: q.Get("connectivity"), Platform: q.Get("platform"), Query: q.Get("q")}
	if !validStatus[f.Status] || !validConnectivity[f.Connectivity] || len(f.Platform) > 32 || len(f.Query) > 100 {
		return httpx.BadRequest("invalid filter")
	}
	items, next, err := s.Devices.List(r.Context(), p.OrgID, f, q.Get("cursor"), limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[devices.Device]{Items: items, NextCursor: next})
	return nil
}

func (s *Server) deviceSummary(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermDevicesRead)
	if err != nil {
		return err
	}
	sum, err := s.Devices.Summary(r.Context(), p.OrgID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, sum)
	return nil
}

func (s *Server) getDevice(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermDevicesRead)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	d, err := s.Devices.Get(r.Context(), s.Pool, p.OrgID, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) patchDevice(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	d, err := s.Devices.Rename(r.Context(), s.actor(r, p), id, in.Name)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) transitionDevice(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	d, err := s.Devices.Transition(r.Context(), s.actor(r, p), id, chi.URLParam(r, "op"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) deviceActivity(w http.ResponseWriter, r *http.Request) error {
	p, err := s.requireAny(r, auth.PermAuditRead, auth.PermDevicesRead)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.Devices.Get(r.Context(), s.Pool, p.OrgID, id); err != nil {
		return err
	}
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return err
	}
	f := auditFilter{TargetID: &id}
	if err := f.parseCursor(r.URL.Query().Get("cursor")); err != nil {
		return err
	}
	items, next, err := s.queryAudit(r.Context(), p.OrgID, f, limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[auditEntry]{Items: items, NextCursor: next})
	return nil
}

// ---------------------------------------------------------------- commands

func (s *Server) listCommands(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermCommandsRead)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
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
	items, next, err := s.Commands.ListForDevice(r.Context(), p.OrgID, id, before, limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[commands.Command]{Items: items, NextCursor: uuidCursor(next)})
	return nil
}

func permForKind(kind string) string {
	if kind == commands.KindExec {
		return auth.PermCommandsExecArbitrary
	}
	return auth.PermCommandsExecAction
}

func (s *Server) createCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in commands.CreateInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	if err := in.Validate(); err != nil {
		return err
	}
	p, err := s.require(r, permForKind(in.Kind))
	if err != nil {
		return err
	}
	if ok, wait := s.commandsRL.Allow(p.UserID.String()); !ok {
		return httpx.RateLimited(wait)
	}
	c, created, err := s.Commands.Create(r.Context(), s.actor(r, p), id, in, strings.TrimSpace(r.Header.Get("Idempotency-Key")))
	if err != nil {
		return err
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	httpx.WriteJSON(w, status, c)
	return nil
}

func (s *Server) getCommand(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermCommandsRead)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	c, err := s.Commands.Get(r.Context(), s.Pool, p.OrgID, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

func (s *Server) cancelCommand(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermCommandsRead)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	c, err := s.Commands.Get(r.Context(), s.Pool, p.OrgID, id)
	if err != nil {
		return err
	}
	if p, err = s.require(r, permForKind(c.Kind)); err != nil {
		return err
	}
	out, err := s.Commands.Cancel(r.Context(), s.actor(r, p), c)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// ---------------------------------------------------------------- enrollment

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermEnrollmentManage)
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
	items, next, err := s.Enrollment.List(r.Context(), p.OrgID, before, limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[enrollment.Token]{Items: items, NextCursor: uuidCursor(next)})
	return nil
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermEnrollmentManage)
	if err != nil {
		return err
	}
	var in enrollment.CreateInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	t, err := s.Enrollment.Create(r.Context(), s.actor(r, p), in)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, t)
	return nil
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermEnrollmentManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Enrollment.Revoke(r.Context(), s.actor(r, p), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---------------------------------------------------------------- users

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.require(r, auth.PermUsersManage); err != nil {
		return err
	}
	roles, err := s.Users.Roles(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, roles)
	return nil
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermUsersManage)
	if err != nil {
		return err
	}
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return err
	}
	after, err := cursorUUID(r)
	if err != nil {
		return err
	}
	items, next, err := s.Users.List(r.Context(), p.OrgID, after, limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[users.User]{Items: items, NextCursor: uuidCursor(next)})
	return nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermUsersManage)
	if err != nil {
		return err
	}
	var in users.CreateInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	u, err := s.Users.Create(r.Context(), p, in, httpx.RequestID(r.Context()), httpx.ClientIP(r.Context()))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, u)
	return nil
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermUsersManage)
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in users.UpdateInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	u, err := s.Users.Update(r.Context(), p, id, in, httpx.RequestID(r.Context()), httpx.ClientIP(r.Context()))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}
