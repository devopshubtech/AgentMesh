package auth

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
)

// Permission keys. Handlers check permissions, never role names.
const (
	PermDevicesRead           = "devices.read"
	PermDevicesManage         = "devices.manage"
	PermCommandsRead          = "commands.read"
	PermCommandsExecAction    = "commands.execute.action"
	PermCommandsExecArbitrary = "commands.execute.exec"
	PermEnrollmentManage      = "enrollment.manage"
	PermUsersManage           = "users.manage"
	PermAuditRead             = "audit.read"
	PermPlatformAdmin         = "platform.admin"
)

// Principal is the authenticated user behind a request.
type Principal struct {
	UserID      uuid.UUID
	OrgID       uuid.UUID
	Email       string
	Role        string
	SessionID   uuid.UUID
	Permissions map[string]bool
}

// Can reports whether the principal holds perm.
func (p *Principal) Can(perm string) bool { return p != nil && p.Permissions[perm] }

// PermissionList returns the sorted permission keys.
func (p *Principal) PermissionList() []string {
	out := make([]string, 0, len(p.Permissions))
	for k := range p.Permissions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// HoldsAll reports whether the principal holds every permission in perms.
func (p *Principal) HoldsAll(perms []string) bool {
	for _, k := range perms {
		if !p.Permissions[k] {
			return false
		}
	}
	return true
}

type principalKey struct{}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the request principal, or nil.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// Require returns a 403 unless the request principal holds perm.
func Require(ctx context.Context, perm string) (*Principal, error) {
	p := FromContext(ctx)
	if p == nil {
		return nil, httpx.Unauthorized("authentication required")
	}
	if !p.Can(perm) {
		return nil, httpx.Forbidden("missing permission " + perm)
	}
	return p, nil
}
