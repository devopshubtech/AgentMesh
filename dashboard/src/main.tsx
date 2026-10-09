import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { RouterProvider } from 'react-router/dom';
import { AppProviders } from '@/app/providers';
import { router } from '@/app/router';
import { initTheme } from '@/lib/theme';
import { setSession } from '@/auth/session';
import { DEMO_USER } from '@/demo/data';
import { enterDemo, isDemo } from '@/demo/mode';
import './index.css';

// Applied before first render (no inline script needed, which keeps the strict CSP intact).
initTheme();

// ?demo=1 opens the demo directly (shareable link); the flag then stays for this tab.
const params = new URLSearchParams(window.location.search);
if (params.has('demo')) {
  enterDemo();
  params.delete('demo');
  const qs = params.toString();
  window.history.replaceState(null, '', window.location.pathname + (qs ? `?${qs}` : '') + window.location.hash);
}

// A tab in demo mode (see src/demo) is signed in as the sample admin, locally.
if (isDemo()) setSession('demo', 365 * 86400, DEMO_USER);

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('#root element missing');

createRoot(rootEl).render(
  <StrictMode>
    <AppProviders>
      <RouterProvider router={router} />
    </AppProviders>
  </StrictMode>,
);
