import { useRef, type KeyboardEvent, type ReactNode } from 'react';
import { cn } from '@/lib/cn';

export interface TabDef<T extends string> {
  value: T;
  label: ReactNode;
  disabled?: boolean;
  hint?: string;
}

/** Accessible tab list (roving focus with arrow keys). Content is rendered by the caller. */
export function Tabs<T extends string>({
  tabs,
  value,
  onChange,
  idPrefix,
}: {
  tabs: readonly TabDef<T>[];
  value: T;
  onChange: (v: T) => void;
  idPrefix: string;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  const onKey = (e: KeyboardEvent, index: number) => {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
    e.preventDefault();
    const dir = e.key === 'ArrowRight' ? 1 : -1;
    for (let step = 1; step <= tabs.length; step++) {
      const i = (index + dir * step + tabs.length) % tabs.length;
      const t = tabs[i];
      if (t && !t.disabled) {
        refs.current[i]?.focus();
        onChange(t.value);
        break;
      }
    }
  };

  return (
    <div role="tablist" className="flex gap-1 overflow-x-auto border-b border-border">
      {tabs.map((t, i) => {
        const selected = t.value === value;
        return (
          <button
            key={t.value}
            ref={(el) => {
              refs.current[i] = el;
            }}
            id={`${idPrefix}-tab-${t.value}`}
            role="tab"
            type="button"
            aria-selected={selected}
            aria-controls={`${idPrefix}-panel-${t.value}`}
            aria-disabled={t.disabled || undefined}
            tabIndex={selected ? 0 : -1}
            disabled={t.disabled}
            title={t.hint}
            onClick={() => !t.disabled && onChange(t.value)}
            onKeyDown={(e) => onKey(e, i)}
            className={cn(
              '-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm font-medium whitespace-nowrap transition-colors',
              selected
                ? 'border-primary text-fg'
                : 'border-transparent text-muted hover:border-border hover:text-fg',
              t.disabled && 'cursor-not-allowed opacity-50 hover:border-transparent hover:text-muted',
            )}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}

export function TabPanel({
  idPrefix,
  value,
  children,
}: {
  idPrefix: string;
  value: string;
  children: ReactNode;
}) {
  return (
    <div
      role="tabpanel"
      id={`${idPrefix}-panel-${value}`}
      aria-labelledby={`${idPrefix}-tab-${value}`}
      className="pt-4"
    >
      {children}
    </div>
  );
}
