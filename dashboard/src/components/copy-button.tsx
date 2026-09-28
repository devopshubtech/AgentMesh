import { useEffect, useState } from 'react';
import { Check, Copy } from 'lucide-react';
import { copyToClipboard } from '@/lib/hooks';
import { Button } from './ui/button';
import { toast } from './ui/toast-store';

export function CopyButton({ text, label = 'Copy' }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <Button
      variant="outline"
      size="sm"
      icon={copied ? <Check className="size-3.5 text-emerald-600" /> : <Copy className="size-3.5" />}
      onClick={() =>
        void copyToClipboard(text).then((ok) => {
          if (ok) setCopied(true);
          else toast.error('Copy failed', 'Select the text and copy it manually.');
        })
      }
    >
      {copied ? 'Copied' : label}
    </Button>
  );
}

/** Monospace, selectable code line with a copy button. */
export function CopyField({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium text-muted">{label}</span>
        <CopyButton text={value} />
      </div>
      <pre className="overflow-x-auto rounded-md bg-code-bg p-3 font-mono text-xs whitespace-pre-wrap break-all text-code-fg select-all">
        {value}
      </pre>
    </div>
  );
}
