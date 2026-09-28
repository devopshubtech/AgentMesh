package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
)

type auditEntry struct {
	ID         int64           `json:"id"`
	TS         time.Time       `json:"ts"`
	RequestID  *string         `json:"request_id"`
	ActorType  string          `json:"actor_type"`
	ActorID    *uuid.UUID      `json:"actor_id"`
	ActorLabel *string         `json:"actor_label"`
	ActorIP    *string         `json:"actor_ip"`
	Action     string          `json:"action"`
	TargetType *string         `json:"target_type"`
	TargetID   *uuid.UUID      `json:"target_id"`
	Outcome    string          `json:"outcome"`
	Details    json.RawMessage `json:"details"`
}

type auditFilter struct {
	Action   string
	ActorID  *uuid.UUID
	TargetID *uuid.UUID
	Outcome  string
	From, To *time.Time
	Before   *int64
}

func (f *auditFilter) parseCursor(c string) error {
	if c == "" {
		return nil
	}
	n, err := strconv.ParseInt(c, 10, 64)
	if err != nil || n <= 0 {
		return httpx.BadRequest("invalid cursor")
	}
	f.Before = &n
	return nil
}

func (s *Server) queryAudit(ctx context.Context, orgID uuid.UUID, f auditFilter, limit int) ([]auditEntry, *string, error) {
	var actionExact, actionPrefix *string
	if f.Action != "" {
		if p, ok := strings.CutSuffix(f.Action, "*"); ok {
			esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(p) + "%"
			actionPrefix = &esc
		} else {
			actionExact = &f.Action
		}
	}
	var outcome *string
	if f.Outcome != "" {
		outcome = &f.Outcome
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, ts, request_id, actor_type, actor_id, actor_label, actor_ip, action, target_type, target_id, outcome, details
		FROM audit_logs
		WHERE org_id = $1
		  AND ($2::bigint IS NULL OR id < $2)
		  AND ($3::text IS NULL OR action = $3)
		  AND ($4::text IS NULL OR action LIKE $4)
		  AND ($5::uuid IS NULL OR actor_id = $5)
		  AND ($6::uuid IS NULL OR target_id = $6)
		  AND ($7::text IS NULL OR outcome = $7)
		  AND ($8::timestamptz IS NULL OR ts >= $8)
		  AND ($9::timestamptz IS NULL OR ts < $9)
		ORDER BY id DESC LIMIT $10`,
		orgID, f.Before, actionExact, actionPrefix, f.ActorID, f.TargetID, outcome, f.From, f.To, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []auditEntry{}
	for rows.Next() {
		var e auditEntry
		var details []byte
		if err := rows.Scan(&e.ID, &e.TS, &e.RequestID, &e.ActorType, &e.ActorID, &e.ActorLabel, &e.ActorIP, &e.Action,
			&e.TargetType, &e.TargetID, &e.Outcome, &details); err != nil {
			return nil, nil, err
		}
		e.Details = details
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		n := strconv.FormatInt(out[limit-1].ID, 10)
		next = &n
	}
	return out, next, nil
}

func parseOptUUID(v httpx.Validation, field, s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		v[field] = "must be a UUID"
		return nil
	}
	return &id
}

func parseOptTime(v httpx.Validation, field, s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		v[field] = "must be an RFC 3339 timestamp"
		return nil
	}
	return &t
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermAuditRead)
	if err != nil {
		return err
	}
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	v := httpx.Validation{}
	f := auditFilter{
		Action:   q.Get("action"),
		ActorID:  parseOptUUID(v, "actor_id", q.Get("actor_id")),
		TargetID: parseOptUUID(v, "target_id", q.Get("target_id")),
		Outcome:  q.Get("outcome"),
		From:     parseOptTime(v, "from", q.Get("from")),
		To:       parseOptTime(v, "to", q.Get("to")),
	}
	if len(f.Action) > 100 {
		v["action"] = "too long"
	}
	if f.Outcome != "" && f.Outcome != audit.Success && f.Outcome != audit.Denied && f.Outcome != audit.Failure {
		v["outcome"] = "must be success, denied or error"
	}
	if err := v.Err(); err != nil {
		return err
	}
	if err := f.parseCursor(q.Get("cursor")); err != nil {
		return err
	}
	items, next, err := s.queryAudit(r.Context(), p.OrgID, f, limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List[auditEntry]{Items: items, NextCursor: next})
	return nil
}

func (s *Server) verifyAudit(w http.ResponseWriter, r *http.Request) error {
	p, err := s.require(r, auth.PermPlatformAdmin)
	if err != nil {
		return err
	}
	res, err := audit.Verify(r.Context(), s.Pool, p.OrgID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}
