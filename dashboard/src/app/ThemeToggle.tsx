import { Monitor, Moon, Sun } from 'lucide-react';
import { setThemePreference, useTheme, type ThemePreference } from '@/lib/theme';

const next: Record<ThemePreference, ThemePreference> = {
  light: 'dark',
  dark: 'system',
  system: 'light',
};

export function ThemeToggle() {
  const { preference } = useTheme();
  const Icon = preference === 'light' ? Sun : preference === 'dark' ? Moon : Monitor;
  const label = `Theme: ${preference} (click to switch to ${next[preference]})`;
  return (
    <button
      type="button"
      onClick={() => setThemePreference(next[preference])}
      className="inline-flex size-8 items-center justify-center rounded-md text-muted hover:bg-subtle hover:text-fg"
      title={label}
      aria-label={label}
    >
      <Icon className="size-4" aria-hidden />
    </button>
  );
}
