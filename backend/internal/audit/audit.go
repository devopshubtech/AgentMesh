// Package audit records an append-only, per-organization hash-chained log of
// security-relevant actions.
//
// Each row stores hash = sha256(prev_hash || canonical(row)). Rows cannot be
// updated or deleted (enforced by a database trigger), and Verify recomputes
// the chain to detect tampering done by bypassing that trigger.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/enfec/agentmesh/backend/internal/platform/db"
)

// Actor types.
const (
	ActorUser   = "user"
	ActorDevice = "device"
	ActorSystem = "system"
)

// Outcomes.
const (
	Success = "success"
	Denied  = "denied"
	Failure = "error"
)

// Event is one audit record.
type Event struct {
	OrgID      uuid.UUID
	RequestID  string
	ActorType  string
	ActorID    *uuid.UUID
	ActorLabel string
	ActorIP    string
	SessionID  *uuid.UUID
	Action     string
	TargetType string
	TargetID   *uuid.UUID
	Outcome    string
	Details    map[string]any
}

// canonical is the hashed representation; field order is fixed by the struct.
type canonical struct {
	OrgID      string          `json:"org_id"`
	TS         string          `json:"ts"`
	RequestID  *string         `json:"request_id"`
	ActorType  string          `json:"actor_type"`
	ActorID    *string         `json:"actor_id"`
	ActorLabel *string         `json:"actor_label"`
	ActorIP    *string         `json:"actor_ip"`
	SessionID  *string         `json:"session_id"`
	Action     string          `json:"action"`
	TargetType *string         `json:"target_type"`
	TargetID   *string         `json:"target_id"`
	Outcome    string          `json:"outcome"`
	Details    json.RawMessage `json:"details"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func uuidStr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

// normalizeDetails produces a representation that survives a jsonb round
// trip byte-for-byte: decode to generic values, re-encode with sorted keys.
func normalizeDetails(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func computeHash(prev []byte, c canonical) ([]byte, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(prev)
	h.Write(b)
	return h.Sum(nil), nil
}

// Record appends e inside tx. The per-org advisory lock serializes writers so
// the chain stays linear; it is released when tx ends.
func Record(ctx context.Context, tx pgx.Tx, e Event) error {
	if e.Outcome == "" {
		e.Outcome = Success
	}
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	rawDetails, err := json.Marshal(e.Details)
	if err != nil {
		return fmt.Errorf("audit details: %w", err)
	}
	details, err := normalizeDetails(rawDetails)
	if err != nil {
		return fmt.Errorf("audit details: %w", err)
	}
	ts := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('audit:' || $1::text, 0))`, e.OrgID); err != nil {
		return fmt.Errorf("audit lock: %w", err)
	}
	var prev []byte
	err = tx.QueryRow(ctx, `SELECT hash FROM audit_logs WHERE org_id = $1 ORDER BY id DESC LIMIT 1`, e.OrgID).Scan(&prev)
	if err != nil && !db.IsNoRows(err) {
		return fmt.Errorf("audit prev hash: %w", err)
	}
	if prev == nil {
		prev = make([]byte, sha256.Size)
	}

	c := canonical{
		OrgID: e.OrgID.String(), TS: ts.Format(time.RFC3339Nano),
		RequestID: strPtr(e.RequestID), ActorType: e.ActorType, ActorID: uuidStr(e.ActorID),
		ActorLabel: strPtr(e.ActorLabel), ActorIP: strPtr(e.ActorIP), SessionID: uuidStr(e.SessionID),
		Action: e.Action, TargetType: strPtr(e.TargetType), TargetID: uuidStr(e.TargetID),
		Outcome: e.Outcome, Details: details,
	}
	hash, err := computeHash(prev, c)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (org_id, ts, request_id, actor_type, actor_id, actor_label, actor_ip, session_id,
		                        action, target_type, target_id, outcome, details, prev_hash, hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		e.OrgID, ts, c.RequestID, e.ActorType, e.ActorID, c.ActorLabel, c.ActorIP, e.SessionID,
		e.Action, c.TargetType, e.TargetID, e.Outcome, []byte(details), prev, hash)
	if err != nil {
		return fmt.Errorf("audit insert: %w", err)
	}
	return nil
}

// RecordStandalone records e in its own transaction.
func RecordStandalone(ctx context.Context, pool *pgxpool.Pool, e Event) error {
	return db.InTx(ctx, pool, func(tx pgx.Tx) error { return Record(ctx, tx, e) })
}

// VerifyResult summarizes a chain verification.
type VerifyResult struct {
	OK         bool   `json:"ok"`
	Checked    int64  `json:"checked"`
	FirstBadID *int64 `json:"first_bad_id"`
}

// Verify recomputes the hash chain of an org from the beginning.
func Verify(ctx context.Context, q db.DBTX, orgID uuid.UUID) (VerifyResult, error) {
	res := VerifyResult{OK: true}
	prev := make([]byte, sha256.Size)
	var lastID int64
	for {
		rows, err := q.Query(ctx, `
			SELECT id, ts, request_id, actor_type, actor_id, actor_label, actor_ip, session_id, action,
			       target_type, target_id, outcome, details::text, prev_hash, hash
			FROM audit_logs WHERE org_id = $1 AND id > $2 ORDER BY id LIMIT 1000`, orgID, lastID)
		if err != nil {
			return res, err
		}
		n := 0
		for rows.Next() {
			n++
			var (
				id                                         int64
				ts                                         time.Time
				requestID, actorLabel, actorIP, targetType *string
				actorType, action, outcome, detailsText    string
				actorID, sessionID, targetID               *uuid.UUID
				prevHash, hash                             []byte
			)
			if err := rows.Scan(&id, &ts, &requestID, &actorType, &actorID, &actorLabel, &actorIP, &sessionID,
				&action, &targetType, &targetID, &outcome, &detailsText, &prevHash, &hash); err != nil {
				rows.Close()
				return res, err
			}
			lastID = id
			res.Checked++
			details, err := normalizeDetails([]byte(detailsText))
			if err != nil {
				rows.Close()
				return res, err
			}
			c := canonical{
				OrgID: orgID.String(), TS: ts.UTC().Format(time.RFC3339Nano), RequestID: requestID,
				ActorType: actorType, ActorID: uuidStr(actorID), ActorLabel: actorLabel, ActorIP: actorIP,
				SessionID: uuidStr(sessionID), Action: action, TargetType: targetType, TargetID: uuidStr(targetID),
				Outcome: outcome, Details: details,
			}
			want, err := computeHash(prev, c)
			if err != nil {
				rows.Close()
				return res, err
			}
			if string(prevHash) != string(prev) || string(hash) != string(want) {
				rows.Close()
				res.OK = false
				res.FirstBadID = &id
				return res, nil
			}
			prev = hash
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return res, err
		}
		if n == 0 {
			return res, nil
		}
	}
}
