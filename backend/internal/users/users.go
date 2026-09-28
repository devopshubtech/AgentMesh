// Package users manages operator accounts and their built-in role assignment.
package users

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/enfec/agentmesh/backend/internal/audit"
	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
)

// DefaultOrgID is the organization created by the initial migration.
var DefaultOrgID = uuid.MustParse("00000000-0000-7000-8000-000000000001")

// User is the API representation of an operator account.
type User struct {
	ID          uuid.UUID  `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Status      string     `json:"status"`
	Role        string     `json:"role"`
	Permissions []string   `json:"permissions"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

// Role is a built-in role with its permissions.
type Role struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// Service implements user management.
type Service struct {
	pool *pgxpool.Pool
}

// NewService constructs a Service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

const selectUser = `
	SELECT u.id, u.email, u.display_name, u.status,
	       coalesce((array_agg(DISTINCT r.name))[1], ''),
	       coalesce(array_agg(DISTINCT rp.permission_key ORDER BY rp.permission_key)
	                FILTER (WHERE rp.permission_key IS NOT NULL), '{}'),
	       u.created_at, u.last_login_at
	FROM users u
	LEFT JOIN user_roles ur ON ur.user_id = u.id
	LEFT JOIN roles r ON r.id = ur.role_id
	LEFT JOIN role_permissions rp ON rp.role_id = r.id`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Status, &u.Role, &u.Permissions, &u.CreatedAt, &u.LastLoginAt); err != nil {
		return nil, err
	}
	return &u, nil
}

// Get returns a user in org.
func (s *Service) Get(ctx context.Context, q db.DBTX, orgID, id uuid.UUID) (*User, error) {
	u, err := scanUser(q.QueryRow(ctx, selectUser+` WHERE u.org_id = $1 AND u.id = $2 GROUP BY u.id`, orgID, id))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("user")
	}
	return u, err
}

// List returns users ordered by id (UUIDv7 = creation order).
func (s *Service) List(ctx context.Context, orgID uuid.UUID, after *uuid.UUID, limit int) ([]User, *uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, selectUser+`
		WHERE u.org_id = $1 AND ($2::uuid IS NULL OR u.id > $2)
		GROUP BY u.id ORDER BY u.id LIMIT $3`, orgID, after, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *uuid.UUID
	if len(out) > limit {
		out = out[:limit]
		id := out[limit-1].ID
		next = &id
	}
	return out, next, nil
}

// Roles lists the built-in roles.
func (s *Service) Roles(ctx context.Context) ([]Role, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.name, r.description,
		       coalesce(array_agg(rp.permission_key ORDER BY rp.permission_key) FILTER (WHERE rp.permission_key IS NOT NULL), '{}')
		FROM roles r LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE r.org_id IS NULL
		GROUP BY r.id ORDER BY count(rp.permission_key) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.Name, &r.Description, &r.Permissions); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func rolePermissions(ctx context.Context, q db.DBTX, role string) (uuid.UUID, []string, error) {
	var id uuid.UUID
	var perms []string
	err := q.QueryRow(ctx, `
		SELECT r.id, coalesce(array_agg(rp.permission_key) FILTER (WHERE rp.permission_key IS NOT NULL), '{}')
		FROM roles r LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE r.org_id IS NULL AND r.name = $1 GROUP BY r.id`, role).Scan(&id, &perms)
	if db.IsNoRows(err) {
		return uuid.Nil, nil, httpx.Validation{"role": "unknown role"}.Err()
	}
	return id, perms, err
}

// CreateInput is the payload for Create.
type CreateInput struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}

func normalizeEmail(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 254 {
		return "", false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return "", false
	}
	return strings.ToLower(s), true
}

// Create adds a user. The actor may only grant roles whose permissions they hold.
func (s *Service) Create(ctx context.Context, actor *auth.Principal, in CreateInput, reqID, ip string) (*User, error) {
	v := httpx.Validation{}
	email, ok := normalizeEmail(in.Email)
	if !ok {
		v["email"] = "must be a valid email address"
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if len(in.DisplayName) > 100 {
		v["display_name"] = "must be at most 100 characters"
	}
	if msg := auth.ValidatePassword(in.Password); msg != "" {
		v["password"] = msg
	}
	if in.Role == "" {
		v["role"] = "required"
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	id := uuid.Must(uuid.NewV7())
	var out *User
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		roleID, perms, err := rolePermissions(ctx, tx, in.Role)
		if err != nil {
			return err
		}
		if !actor.HoldsAll(perms) {
			return httpx.Forbidden("cannot grant a role with permissions you do not hold")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, org_id, email, password_hash, display_name) VALUES ($1, $2, $3, $4, $5)`,
			id, actor.OrgID, email, hash, in.DisplayName); err != nil {
			if db.IsUniqueViolation(err) {
				return httpx.Validation{"email": "already in use"}.Err()
			}
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, id, roleID); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: actor.OrgID, RequestID: reqID, ActorType: audit.ActorUser, ActorID: &actor.UserID, ActorLabel: actor.Email,
			ActorIP: ip, SessionID: &actor.SessionID, Action: "user.create", TargetType: "user", TargetID: &id,
			Details: map[string]any{"email": email, "role": in.Role},
		}); err != nil {
			return err
		}
		out, err = s.Get(ctx, tx, actor.OrgID, id)
		return err
	})
	return out, err
}

// UpdateInput is the payload for Update; nil fields are left unchanged.
type UpdateInput struct {
	DisplayName *string `json:"display_name"`
	Status      *string `json:"status"`
	Role        *string `json:"role"`
	Password    *string `json:"password"`
}

// Update modifies a user, guarding against privilege escalation and self-lockout.
func (s *Service) Update(ctx context.Context, actor *auth.Principal, id uuid.UUID, in UpdateInput, reqID, ip string) (*User, error) {
	v := httpx.Validation{}
	if in.DisplayName != nil {
		t := strings.TrimSpace(*in.DisplayName)
		in.DisplayName = &t
		if len(t) > 100 {
			v["display_name"] = "must be at most 100 characters"
		}
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "disabled" {
		v["status"] = "must be active or disabled"
	}
	if in.Password != nil {
		if msg := auth.ValidatePassword(*in.Password); msg != "" {
			v["password"] = msg
		}
	}
	self := id == actor.UserID
	if self && in.Status != nil && *in.Status != "active" {
		v["status"] = "you cannot disable your own account"
	}
	if self && in.Role != nil {
		v["role"] = "you cannot change your own role"
	}
	if err := v.Err(); err != nil {
		return nil, err
	}

	var out *User
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		target, err := s.Get(ctx, tx, actor.OrgID, id)
		if err != nil {
			return err
		}
		if !actor.HoldsAll(target.Permissions) {
			return httpx.Forbidden("cannot modify a user with permissions you do not hold")
		}
		changes := map[string]any{}
		if in.DisplayName != nil {
			if _, err := tx.Exec(ctx, `UPDATE users SET display_name = $2, updated_at = now() WHERE id = $1`, id, *in.DisplayName); err != nil {
				return err
			}
			changes["display_name"] = *in.DisplayName
		}
		if in.Status != nil && *in.Status != target.Status {
			if _, err := tx.Exec(ctx, `UPDATE users SET status = $2, updated_at = now() WHERE id = $1`, id, *in.Status); err != nil {
				return err
			}
			if *in.Status == "disabled" {
				if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
					return err
				}
			}
			changes["status"] = *in.Status
		}
		if in.Role != nil && *in.Role != target.Role {
			roleID, perms, err := rolePermissions(ctx, tx, *in.Role)
			if err != nil {
				return err
			}
			if !actor.HoldsAll(perms) {
				return httpx.Forbidden("cannot grant a role with permissions you do not hold")
			}
			if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, id, roleID); err != nil {
				return err
			}
			changes["role"] = map[string]string{"from": target.Role, "to": *in.Role}
		}
		if in.Password != nil {
			hash, err := auth.HashPassword(*in.Password)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, hash); err != nil {
				return err
			}
			if !self {
				if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
					return err
				}
			}
			changes["password"] = "reset"
		}
		if len(changes) > 0 {
			if err := audit.Record(ctx, tx, audit.Event{
				OrgID: actor.OrgID, RequestID: reqID, ActorType: audit.ActorUser, ActorID: &actor.UserID, ActorLabel: actor.Email,
				ActorIP: ip, SessionID: &actor.SessionID, Action: "user.update", TargetType: "user", TargetID: &id,
				Details: map[string]any{"email": target.Email, "changes": changes},
			}); err != nil {
				return err
			}
		}
		out, err = s.Get(ctx, tx, actor.OrgID, id)
		return err
	})
	return out, err
}

// EnsureBootstrapAdmin creates the first super admin when no users exist.
func EnsureBootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, email, password string, log *slog.Logger) error {
	if email == "" || password == "" {
		return nil
	}
	email, ok := normalizeEmail(email)
	if !ok {
		return errors.New("bootstrap admin email is invalid")
	}
	if msg := auth.ValidatePassword(password); msg != "" {
		return fmt.Errorf("bootstrap admin password %s", msg)
	}
	return db.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('bootstrap-admin', 0))`); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, org_id, email, password_hash, display_name) VALUES ($1, $2, $3, $4, 'Super Admin')`,
			id, DefaultOrgID, email, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE org_id IS NULL AND name = 'super_admin'`, id); err != nil {
			return err
		}
		log.Info("bootstrap super admin created", "email", email)
		return audit.Record(ctx, tx, audit.Event{
			OrgID: DefaultOrgID, ActorType: audit.ActorSystem, ActorLabel: "bootstrap", Action: "user.create",
			TargetType: "user", TargetID: &id, Details: map[string]any{"email": email, "role": "super_admin", "bootstrap": true},
		})
	})
}
