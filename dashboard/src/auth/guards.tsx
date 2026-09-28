import type { ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router';
import { ShieldOff } from 'lucide-react';
import type { Permission } from '@/api/types';
import { EmptyState, PageLoader } from '@/components/ui/feedback';
import { useAuth, useCan } from './context';

export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth();
  const location = useLocation();
  if (status === 'loading') return <PageLoader label="Restoring session…" />;
  if (status === 'unauthenticated') {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  }
  return <>{children}</>;
}

export function RequirePermission({ perm, children }: { perm: Permission; children: ReactNode }) {
  const allowed = useCan(perm);
  if (!allowed) {
    return (
      <EmptyState
        icon={<ShieldOff className="size-5" aria-hidden />}
        title="You don't have access to this page"
        description={`This page requires the "${perm}" permission. Ask an administrator if you need access.`}
      />
    );
  }
  return <>{children}</>;
}
