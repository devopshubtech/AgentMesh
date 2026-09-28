import { useMemo, useState, type FormEvent } from 'react';
import { Pencil, Plus, Users as UsersIcon } from 'lucide-react';
import { isApiError } from '@/api/client';
import { useCreateUser, useRoles, useUpdateUser, useUsers } from '@/api/users';
import type { Role, UpdateUserRequest, User, UserStatus } from '@/api/types';
import { useAuth } from '@/auth/context';
import { PageHeader } from '@/components/page-header';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Dialog } from '@/components/ui/dialog';
import { EmptyState, ErrorState, LoadMore, Spinner } from '@/components/ui/feedback';
import { Checkbox, Field, Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { Table, TBody, TD, TH, THead, TR, TableMessageRow } from '@/components/ui/table';
import { RelativeTime } from '@/components/ui/time';
import { toast } from '@/components/ui/toast-store';

const MIN_PASSWORD = 12;
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function roleLabel(name: string): string {
  return name.replace(/_/g, ' ');
}

function roleOptions(roles: Role[] | undefined) {
  return (roles ?? []).map((r) => ({
    value: r.name,
    label: r.description ? `${roleLabel(r.name)} — ${r.description}` : roleLabel(r.name),
  }));
}

/** Roles whose permissions are a subset of mine (the API refuses anything else). */
function grantableRoles(roles: Role[] | undefined, mine: string[]): Role[] | undefined {
  return roles?.filter((r) => r.permissions.every((p) => mine.includes(p)));
}

function fieldErrorsOf(err: unknown): Record<string, string> | undefined {
  return isApiError(err) ? err.fields : undefined;
}

function CreateUserDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { user: me } = useAuth();
  const roles = useRoles(open);
  const create = useCreateUser();
  const [email, setEmail] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState('');
  const [touched, setTouched] = useState(false);

  const options = roleOptions(grantableRoles(roles.data, me?.permissions ?? []));
  const selectedRole = role || options.find((o) => o.value === 'viewer')?.value || options[0]?.value || '';
  const fe = fieldErrorsOf(create.error);

  const emailErr = touched && !EMAIL_RE.test(email.trim()) ? 'Enter a valid email address.' : fe?.email;
  const pwErr =
    touched && password.length < MIN_PASSWORD
      ? `Password must be at least ${MIN_PASSWORD} characters.`
      : fe?.password;
  const nameErr = touched && !displayName.trim() ? 'Display name is required.' : fe?.display_name;

  const reset = () => {
    setEmail('');
    setDisplayName('');
    setPassword('');
    setRole('');
    setTouched(false);
    create.reset();
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!EMAIL_RE.test(email.trim()) || password.length < MIN_PASSWORD || !displayName.trim() || !selectedRole) {
      return;
    }
    create.mutate(
      { email: email.trim(), display_name: displayName.trim(), password, role: selectedRole },
      {
        onSuccess: (u) => {
          toast.success('User created', u.email);
          reset();
          onClose();
        },
      },
    );
  };

  return (
    <Dialog open={open} onClose={onClose} title="Create user" dismissable={!create.isPending}>
      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <Field label="Email" error={emailErr}>
          {(id) => (
            <Input
              id={id}
              type="email"
              autoFocus
              autoComplete="off"
              value={email}
              aria-invalid={!!emailErr}
              onChange={(e) => setEmail(e.target.value)}
            />
          )}
        </Field>
        <Field label="Display name" error={nameErr}>
          {(id) => (
            <Input
              id={id}
              value={displayName}
              maxLength={200}
              aria-invalid={!!nameErr}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          )}
        </Field>
        <Field label="Initial password" error={pwErr} hint={`At least ${MIN_PASSWORD} characters.`}>
          {(id) => (
            <Input
              id={id}
              type="password"
              autoComplete="new-password"
              value={password}
              aria-invalid={!!pwErr}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </Field>
        <Field label="Role" error={fe?.role} hint="Only roles within your own permissions can be granted.">
          {(id) =>
            roles.isPending ? (
              <Spinner label="Loading roles…" />
            ) : roles.isError ? (
              <ErrorState error={roles.error} onRetry={() => void roles.refetch()} />
            ) : (
              <Select id={id} value={selectedRole} onChange={(e) => setRole(e.target.value)} options={options} />
            )
          }
        </Field>
        <div className="flex justify-end gap-2 border-t border-border pt-4">
          <Button variant="outline" onClick={onClose} disabled={create.isPending}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending} disabled={!selectedRole}>
            Create user
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function EditUserDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const { user: me } = useAuth();
  const isSelf = me?.id === user.id;
  const roles = useRoles(true);
  const update = useUpdateUser();
  const [displayName, setDisplayName] = useState(user.display_name);
  const [role, setRole] = useState<string>(user.role);
  const [status, setStatus] = useState<UserStatus>(user.status);
  const [resetPw, setResetPw] = useState(false);
  const [password, setPassword] = useState('');

  const grantable = grantableRoles(roles.data, me?.permissions ?? []);
  const options = roleOptions(grantable);
  // Always show the user's current role, even if we could not grant it ourselves.
  if (!options.some((o) => o.value === user.role)) {
    options.unshift({ value: user.role, label: roleLabel(user.role) });
  }
  const fe = fieldErrorsOf(update.error);
  const pwErr =
    resetPw && password.length > 0 && password.length < MIN_PASSWORD
      ? `Password must be at least ${MIN_PASSWORD} characters.`
      : fe?.password;

  const body: UpdateUserRequest = {};
  if (displayName.trim() && displayName.trim() !== user.display_name) body.display_name = displayName.trim();
  if (!isSelf && role !== user.role) body.role = role;
  if (!isSelf && status !== user.status) body.status = status;
  if (resetPw && password.length >= MIN_PASSWORD) body.password = password;
  const hasChanges = Object.keys(body).length > 0;
  const valid = !!displayName.trim() && (!resetPw || password.length >= MIN_PASSWORD);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!hasChanges || !valid) return;
    update.mutate(
      { id: user.id, body },
      {
        onSuccess: () => {
          toast.success('User updated', user.email);
          onClose();
        },
      },
    );
  };

  return (
    <Dialog
      open
      onClose={onClose}
      title="Edit user"
      description={<span className="font-mono text-xs">{user.email}</span>}
      dismissable={!update.isPending}
    >
      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <Field label="Display name" error={!displayName.trim() ? 'Display name is required.' : fe?.display_name}>
          {(id) => (
            <Input id={id} value={displayName} maxLength={200} onChange={(e) => setDisplayName(e.target.value)} />
          )}
        </Field>
        <Field
          label="Role"
          error={fe?.role}
          hint={isSelf ? 'You cannot change your own role.' : undefined}
        >
          {(id) =>
            roles.isPending ? (
              <Spinner label="Loading roles…" />
            ) : (
              <Select
                id={id}
                value={role}
                disabled={isSelf}
                onChange={(e) => setRole(e.target.value)}
                options={options}
              />
            )
          }
        </Field>
        <Field
          label="Status"
          error={fe?.status}
          hint={isSelf ? 'You cannot disable yourself.' : 'Disabled users cannot sign in.'}
        >
          {(id) => (
            <Select
              id={id}
              value={status}
              disabled={isSelf}
              onChange={(e) => setStatus(e.target.value as UserStatus)}
              options={[
                { value: 'active', label: 'Active' },
                { value: 'disabled', label: 'Disabled' },
              ]}
            />
          )}
        </Field>
        <div className="flex flex-col gap-2">
          <Checkbox label="Reset password" checked={resetPw} onChange={(e) => setResetPw(e.target.checked)} />
          {resetPw && (
            <Field label="New password" error={pwErr} hint={`At least ${MIN_PASSWORD} characters.`}>
              {(id) => (
                <Input
                  id={id}
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  aria-invalid={!!pwErr}
                  onChange={(e) => setPassword(e.target.value)}
                />
              )}
            </Field>
          )}
        </div>
        <div className="flex justify-end gap-2 border-t border-border pt-4">
          <Button variant="outline" onClick={onClose} disabled={update.isPending}>
            Cancel
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!hasChanges || !valid}>
            Save changes
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

export function UsersPage() {
  const { user: me } = useAuth();
  const users = useUsers();
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<User | null>(null);
  const items = useMemo(() => users.data?.pages.flatMap((p) => p.items) ?? [], [users.data]);

  return (
    <>
      <PageHeader
        title="Users"
        description="People who can sign in to the AgentMesh console."
        actions={
          <Button icon={<Plus className="size-4" />} onClick={() => setCreateOpen(true)}>
            New user
          </Button>
        }
      />
      <Card>
        <Table>
          <THead>
            <tr>
              <TH>User</TH>
              <TH>Role</TH>
              <TH>Status</TH>
              <TH>Last login</TH>
              <TH>Created</TH>
              <TH className="text-right">
                <span className="sr-only">Actions</span>
              </TH>
            </tr>
          </THead>
          <TBody>
            {users.isPending ? (
              <TableMessageRow colSpan={6}>
                <Spinner label="Loading users…" />
              </TableMessageRow>
            ) : users.isError ? (
              <tr>
                <td colSpan={6}>
                  <ErrorState error={users.error} onRetry={() => void users.refetch()} />
                </td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={6}>
                  <EmptyState icon={<UsersIcon className="size-5" />} title="No users" />
                </td>
              </tr>
            ) : (
              items.map((u) => (
                <TR key={u.id} className={u.status === 'disabled' ? 'opacity-60' : undefined}>
                  <TD>
                    <div className="font-medium">
                      {u.display_name}
                      {u.id === me?.id && (
                        <Badge tone="gray" className="ml-2">
                          you
                        </Badge>
                      )}
                    </div>
                    <div className="text-xs text-muted">{u.email}</div>
                  </TD>
                  <TD>
                    <Badge tone={u.role === 'super_admin' || u.role === 'admin' ? 'purple' : 'blue'}>
                      {roleLabel(u.role)}
                    </Badge>
                  </TD>
                  <TD>
                    <Badge tone={u.status === 'active' ? 'green' : 'gray'}>{u.status}</Badge>
                  </TD>
                  <TD className="text-muted">
                    <RelativeTime value={u.last_login_at} />
                  </TD>
                  <TD className="text-muted">
                    <RelativeTime value={u.created_at} />
                  </TD>
                  <TD className="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      icon={<Pencil className="size-3.5" />}
                      onClick={() => setEditing(u)}
                    >
                      Edit
                    </Button>
                  </TD>
                </TR>
              ))
            )}
          </TBody>
        </Table>
        <LoadMore
          hasNextPage={users.hasNextPage}
          isFetchingNextPage={users.isFetchingNextPage}
          fetchNextPage={users.fetchNextPage}
        />
      </Card>
      <CreateUserDialog open={createOpen} onClose={() => setCreateOpen(false)} />
      {editing && <EditUserDialog key={editing.id} user={editing} onClose={() => setEditing(null)} />}
    </>
  );
}
