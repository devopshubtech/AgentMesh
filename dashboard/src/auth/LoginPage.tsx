import { useState, type FormEvent } from 'react';
import { Navigate, useLocation } from 'react-router';
import { AlertCircle, Network } from 'lucide-react';
import { isApiError } from '@/api/client';
import { Button } from '@/components/ui/button';
import { Field, Input } from '@/components/ui/input';
import { PageLoader } from '@/components/ui/feedback';
import { errorMessage } from '@/components/ui/toast-store';
import { ThemeToggle } from '@/app/ThemeToggle';
import { useAuth } from './context';

function describeLoginError(err: unknown): { message: string; requestId: string | null } {
  if (isApiError(err)) {
    if (err.code === 'invalid_credentials') {
      return { message: 'Incorrect email or password.', requestId: null };
    }
    if (err.code === 'rate_limited') {
      const wait = err.retryAfterSec
        ? ` Try again in ${err.retryAfterSec >= 60 ? `${Math.ceil(err.retryAfterSec / 60)} min` : `${err.retryAfterSec} s`}.`
        : ' Please wait a moment and try again.';
      return { message: `Too many sign-in attempts.${wait}`, requestId: err.requestId };
    }
    return { message: errorMessage(err), requestId: err.requestId };
  }
  return { message: errorMessage(err), requestId: null };
}

export function LoginPage() {
  const { status, login, bootError } = useAuth();
  const location = useLocation();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<unknown>(null);

  if (status === 'loading') return <PageLoader label="Restoring session…" />;
  if (status === 'authenticated') {
    const from = (location.state as { from?: string } | null)?.from;
    return <Navigate to={from && from.startsWith('/') && !from.startsWith('//') ? from : '/devices'} replace />;
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await login(email.trim(), password);
      setPassword('');
    } catch (err) {
      setError(err);
    } finally {
      setSubmitting(false);
    }
  };

  const shown = error ?? bootError;
  const desc = shown ? describeLoginError(shown) : null;

  return (
    <div className="relative flex min-h-full items-center justify-center bg-bg px-4 py-12">
      <div className="absolute top-4 right-4">
        <ThemeToggle />
      </div>
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3 text-center">
          <div className="flex size-11 items-center justify-center rounded-xl bg-primary text-primary-fg shadow">
            <Network className="size-6" aria-hidden />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-fg">Sign in to AgentMesh</h1>
            <p className="mt-1 text-sm text-muted">Device management console</p>
          </div>
        </div>
        <form
          onSubmit={(e) => void onSubmit(e)}
          className="flex flex-col gap-4 rounded-xl border border-border bg-surface p-6 shadow-sm"
          noValidate
        >
          {desc && (
            <div
              role="alert"
              className="flex gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
            >
              <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
              <div>
                <p>{error ? desc.message : `Could not restore session: ${desc.message}`}</p>
                {desc.requestId && (
                  <p className="mt-1 font-mono text-xs opacity-80">request_id: {desc.requestId}</p>
                )}
              </div>
            </div>
          )}
          <Field label="Email">
            {(id) => (
              <Input
                id={id}
                type="email"
                autoComplete="username"
                required
                autoFocus
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            )}
          </Field>
          <Field label="Password">
            {(id) => (
              <Input
                id={id}
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            )}
          </Field>
          <Button type="submit" loading={submitting} disabled={!email || !password} className="mt-2 w-full">
            Sign in
          </Button>
        </form>
      </div>
    </div>
  );
}
