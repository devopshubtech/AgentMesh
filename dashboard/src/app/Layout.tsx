import { useState } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router';
import {
  KeyRound,
  LogOut,
  Menu,
  Monitor,
  Network,
  ScrollText,
  Users as UsersIcon,
  X,
  Smartphone,
  type LucideIcon,
} from 'lucide-react';
import type { Permission } from '@/api/types';
import { useLiveEvents } from '@/api/events';
import { useConfig } from '@/api/config';
import { useAuth, usePermissions } from '@/auth/context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/cn';
import { ThemeToggle } from './ThemeToggle';

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  perm: Permission;
}

const NAV: NavItem[] = [
  { to: '/connect', label: 'Connect a phone', icon: Smartphone, perm: 'sessions.exit_node' },
  { to: '/devices', label: 'Devices', icon: Monitor, perm: 'devices.read' },
  { to: '/enrollment', label: 'Enrollment', icon: KeyRound, perm: 'enrollment.manage' },
  { to: '/audit', label: 'Audit log', icon: ScrollText, perm: 'audit.read' },
  { to: '/users', label: 'Users', icon: UsersIcon, perm: 'users.manage' },
];

function LiveIndicator({ status }: { status: 'connecting' | 'open' | 'closed' }) {
  const label = status === 'open' ? 'Live' : status === 'connecting' ? 'Connecting…' : 'Reconnecting…';
  return (
    <span
      className="hidden items-center gap-1.5 text-xs text-muted sm:inline-flex"
      title="Live event stream status"
    >
      <span
        className={cn(
          'size-2 rounded-full',
          status === 'open' ? 'bg-emerald-500' : 'animate-pulse bg-amber-500',
        )}
        aria-hidden
      />
      {label}
    </span>
  );
}

export function Layout() {
  const { user, logout } = useAuth();
  const can = usePermissions();
  const navigate = useNavigate();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const liveStatus = useLiveEvents(!!user);
  const config = useConfig();

  const items = NAV.filter((n) => can(n.perm));

  const onLogout = async () => {
    setLoggingOut(true);
    try {
      await logout();
    } finally {
      setLoggingOut(false);
      void navigate('/login', { replace: true });
    }
  };

  const sidebar = (
    <nav className="flex h-full flex-col gap-1 p-3" aria-label="Main">
      <div className="mb-4 flex items-center gap-2 px-2 py-1">
        <div className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-fg">
          <Network className="size-4.5" aria-hidden />
        </div>
        <span className="text-base font-semibold text-fg">AgentMesh</span>
      </div>
      {items.map((n) => (
        <NavLink
          key={n.to}
          to={n.to}
          onClick={() => setMobileOpen(false)}
          className={({ isActive }) =>
            cn(
              'flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm font-medium transition-colors',
              isActive ? 'bg-primary/10 text-primary' : 'text-muted hover:bg-subtle hover:text-fg',
            )
          }
        >
          <n.icon className="size-4" aria-hidden />
          {n.label}
        </NavLink>
      ))}
      <div className="mt-auto px-2.5 pt-4 text-xs text-muted">
        {config.data?.version ? `control-api v${config.data.version}` : null}
      </div>
    </nav>
  );

  return (
    <div className="flex h-full">
      <aside className="hidden w-56 shrink-0 border-r border-border bg-surface md:block">{sidebar}</aside>

      {mobileOpen && (
        <div className="fixed inset-0 z-40 md:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={() => setMobileOpen(false)} aria-hidden />
          <aside className="absolute inset-y-0 left-0 w-64 border-r border-border bg-surface shadow-xl">
            <button
              type="button"
              className="absolute top-3 right-3 rounded p-1 text-muted hover:bg-subtle"
              onClick={() => setMobileOpen(false)}
              aria-label="Close menu"
            >
              <X className="size-4" />
            </button>
            {sidebar}
          </aside>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-surface px-4">
          <button
            type="button"
            className="rounded p-1.5 text-muted hover:bg-subtle md:hidden"
            onClick={() => setMobileOpen(true)}
            aria-label="Open menu"
          >
            <Menu className="size-5" />
          </button>
          <div className="flex-1" />
          <LiveIndicator status={liveStatus} />
          <ThemeToggle />
          {user && (
            <div className="flex min-w-0 items-center gap-2 border-l border-border pl-3">
              <div className="hidden min-w-0 flex-col items-end leading-tight sm:flex">
                <span className="truncate text-sm font-medium text-fg" title={user.email}>
                  {user.email}
                </span>
                <span className="text-xs text-muted">{user.display_name}</span>
              </div>
              <Badge tone="blue">{user.role.replace('_', ' ')}</Badge>
            </div>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => void onLogout()}
            loading={loggingOut}
            icon={<LogOut className="size-4" aria-hidden />}
          >
            <span className="hidden sm:inline">Log out</span>
          </Button>
        </header>
        <main className="min-w-0 flex-1 overflow-y-auto">
          <div className="mx-auto max-w-7xl p-4 md:p-6">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}
