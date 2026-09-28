import { useEffect, useRef, useState, type ReactNode } from 'react';
import { X } from 'lucide-react';
import { cn } from '@/lib/cn';
import { Button } from './button';
import { Input } from './input';

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  size?: 'sm' | 'md' | 'lg' | 'xl';
  /** Prevent closing via Esc / backdrop (e.g. while a request is in flight). */
  dismissable?: boolean;
}

const widths = { sm: 'max-w-md', md: 'max-w-lg', lg: 'max-w-2xl', xl: 'max-w-4xl' } as const;

/** Modal built on the native <dialog> element (focus trap, Esc, top layer for free). */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = 'md',
  dismissable = true,
}: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      onCancel={(e) => {
        e.preventDefault();
        if (dismissable) onClose();
      }}
      onMouseDown={(e) => {
        if (dismissable && e.target === ref.current) onClose();
      }}
      className={cn(
        'm-auto w-[calc(100%-2rem)] rounded-xl border border-border bg-surface p-0 text-fg shadow-2xl',
        widths[size],
      )}
    >
      {open && (
        <div className="flex max-h-[85vh] flex-col">
          <div className="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
            <div className="min-w-0">
              <h2 className="text-base font-semibold">{title}</h2>
              {description && <div className="mt-1 text-sm text-muted">{description}</div>}
            </div>
            {dismissable && (
              <button
                type="button"
                onClick={onClose}
                className="rounded p-1 text-muted hover:bg-subtle hover:text-fg"
                aria-label="Close dialog"
              >
                <X className="size-4" />
              </button>
            )}
          </div>
          <div className="overflow-y-auto px-5 py-4">{children}</div>
          {footer && (
            <div className="flex justify-end gap-2 border-t border-border bg-subtle/40 px-5 py-3">
              {footer}
            </div>
          )}
        </div>
      )}
    </dialog>
  );
}

export interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel?: string;
  destructive?: boolean;
  loading?: boolean;
  /** When set, the user must type this exact text to enable the confirm button. */
  confirmText?: string;
}

export function ConfirmDialog(props: ConfirmDialogProps) {
  // Remount the body on each open so the typed confirmation resets.
  return (
    <Dialog
      open={props.open}
      onClose={props.onClose}
      title={props.title}
      size="sm"
      dismissable={!props.loading}
    >
      {props.open && <ConfirmBody {...props} />}
    </Dialog>
  );
}

function ConfirmBody({
  onClose,
  onConfirm,
  description,
  confirmLabel = 'Confirm',
  destructive,
  loading,
  confirmText,
}: ConfirmDialogProps) {
  const [typed, setTyped] = useState('');
  const matches = confirmText === undefined || typed === confirmText;
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (matches && !loading) onConfirm();
      }}
      className="flex flex-col gap-4"
    >
      {description && <div className="text-sm text-muted">{description}</div>}
      {confirmText !== undefined && (
        <div className="flex flex-col gap-1.5">
          <label className="text-sm text-fg">
            Type <span className="rounded bg-subtle px-1 font-mono font-semibold">{confirmText}</span>{' '}
            to confirm
          </label>
          <Input
            autoFocus
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
            spellCheck={false}
            aria-label="Confirmation text"
          />
        </div>
      )}
      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={onClose} disabled={loading}>
          Cancel
        </Button>
        <Button
          type="submit"
          variant={destructive ? 'danger' : 'primary'}
          disabled={!matches}
          loading={loading}
          autoFocus={confirmText === undefined}
        >
          {confirmLabel}
        </Button>
      </div>
    </form>
  );
}
