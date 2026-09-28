import { createBrowserRouter } from 'react-router';
import { LoginPage } from '@/auth/LoginPage';
import { RequireAuth, RequirePermission } from '@/auth/guards';
import { DevicesPage } from '@/features/devices/DevicesPage';
import { DeviceDetailPage } from '@/features/devices/DeviceDetailPage';
import { EnrollmentPage } from '@/features/enrollment/EnrollmentPage';
import { AuditPage } from '@/features/audit/AuditPage';
import { UsersPage } from '@/features/users/UsersPage';
import { Layout } from './Layout';
import { HomeRedirect, NotFound, RouteError } from './route-components';

export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage />, errorElement: <RouteError /> },
  {
    path: '/',
    element: (
      <RequireAuth>
        <Layout />
      </RequireAuth>
    ),
    errorElement: <RouteError />,
    children: [
      { index: true, element: <HomeRedirect /> },
      {
        path: 'devices',
        element: (
          <RequirePermission perm="devices.read">
            <DevicesPage />
          </RequirePermission>
        ),
      },
      {
        path: 'devices/:id',
        element: (
          <RequirePermission perm="devices.read">
            <DeviceDetailPage />
          </RequirePermission>
        ),
      },
      {
        path: 'enrollment',
        element: (
          <RequirePermission perm="enrollment.manage">
            <EnrollmentPage />
          </RequirePermission>
        ),
      },
      {
        path: 'audit',
        element: (
          <RequirePermission perm="audit.read">
            <AuditPage />
          </RequirePermission>
        ),
      },
      {
        path: 'users',
        element: (
          <RequirePermission perm="users.manage">
            <UsersPage />
          </RequirePermission>
        ),
      },
      { path: '*', element: <NotFound /> },
    ],
  },
]);
