import { Link, Navigate, isRouteErrorResponse, useRouteError } from 'react-router';
import { usePermissions } from '@/auth/context';
import { EmptyState } from '@/components/ui/feedback';

/** Sends the user to the first section they are allowed to see. */
export function HomeRedirect() {
  const can = usePermissions();
  if (can('devices.read')) return <Navigate to="/devices" replace />;
  if (can('enrollment.manage')) return <Navigate to="/enrollment" replace />;
  if (can('audit.read')) return <Navigate to="/audit" replace />;
  if (can('users.manage')) return <Navigate to="/users" replace />;
  return (
    <EmptyState
      title="No sections available"
      description="Your account has no permissions assigned. Contact an administrator."
    />
  );
}

export function RouteError() {
  const err = useRouteError();
  const notFound = isRouteErrorResponse(err) && err.status === 404;
  return (
    <div className="flex min-h-full items-center justify-center p-6">
      <EmptyState
        title={notFound ? 'Page not found' : 'Unexpected application error'}
        description={
          notFound
            ? 'The page you are looking for does not exist.'
            : err instanceof Error
              ? err.message
              : 'Please reload the page.'
        }
        action={
          <Link to="/" className="text-sm font-medium text-primary hover:underline">
            Go to dashboard
          </Link>
        }
      />
    </div>
  );
}

export function NotFound() {
  return (
    <EmptyState
      title="Page not found"
      description="The page you are looking for does not exist."
      action={
        <Link to="/" className="text-sm font-medium text-primary hover:underline">
          Go to dashboard
        </Link>
      }
    />
  );
}
