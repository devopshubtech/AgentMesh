import { useEffect, useState, type ReactNode } from 'react';
import {
  ArrowDownToLine,
  Battery,
  Check,
  ChevronLeft,
  Download,
  Image as ImageIcon,
  KeyRound,
  Pause,
  Play,
  QrCode,
  RotateCcw,
  ShieldCheck,
  Signal,
  Wifi,
} from 'lucide-react';
import { cn } from '@/lib/cn';
import { QrArt } from './QrArt';

/**
 * A video-like, auto-playing walkthrough of the Android app: install, open,
 * connect by QR code / gallery / pairing code, allow the VPN, connected.
 * The screens mirror the real app (mobile/android-control) and Android's
 * own install and VPN dialogs; they are illustrations, not screenshots.
 */

const STEP_MS = 4500;
const INDIGO = 'bg-[#3949AB]';

interface Step {
  title: string;
  caption: string;
  screen: () => ReactNode;
}

const STEPS: Step[] = [
  {
    title: 'Download the app',
    caption: 'On the phone, open the download link from this page. Chrome saves agentmesh-control.apk.',
    screen: () => <DownloadScreen />,
  },
  {
    title: 'Allow installing',
    caption: 'The first time, Android asks to allow installing apps from Chrome. Turn on "Allow from this source".',
    screen: () => <AllowScreen />,
  },
  {
    title: 'Install',
    caption: 'Tap Install. AgentMesh is small (about 36 MB) and needs Android 8 or newer.',
    screen: () => <InstallScreen />,
  },
  {
    title: 'Open AgentMesh',
    caption: 'Open the app and tap "Connect to remote server". No account or password is needed on the phone.',
    screen: () => <WelcomeScreen />,
  },
  {
    title: 'Choose how to connect',
    caption: 'Scan the QR code, pick a screenshot of it from the gallery, or type the 6-digit pairing code.',
    screen: () => <ConnectDialog />,
  },
  {
    title: 'Scan the QR code',
    caption: 'Point the camera at the QR code shown in the dashboard under Connect a phone.',
    screen: () => <ScanScreen />,
  },
  {
    title: '…or pick it from the gallery',
    caption: 'Got the QR code as a screenshot or a WhatsApp image? Choose it from the gallery instead.',
    screen: () => <GalleryScreen />,
  },
  {
    title: '…or type the pairing code',
    caption: 'No camera needed: type the 6-digit code from the dashboard. It works for 15 minutes.',
    screen: () => <PairScreen />,
  },
  {
    title: 'Allow the VPN',
    caption: 'Android asks once to allow AgentMesh to set up a VPN. Tap OK.',
    screen: () => <VpnScreen />,
  },
  {
    title: 'Connected',
    caption: "Done. The phone's internet now goes out through your computer; websites see its IP address.",
    screen: () => <ConnectedScreen />,
  },
];

export function AndroidWalkthrough() {
  const [step, setStep] = useState(0);
  const [playing, setPlaying] = useState(() => {
    try {
      return !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    } catch {
      return true;
    }
  });

  useEffect(() => {
    if (!playing) return;
    const t = setTimeout(() => setStep((s) => (s + 1) % STEPS.length), STEP_MS);
    return () => clearTimeout(t);
  }, [playing, step]);

  const go = (i: number) => {
    setStep(i);
    setPlaying(false);
  };
  const current = STEPS[step] ?? STEPS[0]!;

  return (
    <div className={cn('grid items-center gap-10 lg:grid-cols-[1fr_auto]', !playing && 'am-paused')}>
      {/* steps list */}
      <div className="order-2 lg:order-1">
        <ol className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-1">
          {STEPS.map((s, i) => (
            <li key={s.title}>
              <button
                type="button"
                onClick={() => go(i)}
                aria-current={i === step ? 'step' : undefined}
                className={cn(
                  'relative flex w-full items-center gap-3 overflow-hidden rounded-lg px-3 py-2 text-left text-sm transition-colors',
                  i === step ? 'bg-primary/10 font-semibold text-primary' : 'text-muted hover:bg-subtle hover:text-fg',
                )}
              >
                <span
                  className={cn(
                    'flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-bold',
                    i < step ? 'bg-emerald-500 text-white' : i === step ? 'bg-primary text-primary-fg' : 'bg-subtle',
                  )}
                >
                  {i < step ? <Check className="size-3.5" aria-hidden /> : i + 1}
                </span>
                {s.title}
                {i === step && (
                  <span key={`${step}-${playing}`} className={cn('absolute inset-x-0 bottom-0 h-0.5 bg-primary', playing ? 'am-progress' : 'opacity-40')} aria-hidden />
                )}
              </button>
            </li>
          ))}
        </ol>
        <div className="mt-5 flex items-center gap-2">
          <button
            type="button"
            onClick={() => setPlaying((p) => !p)}
            className="inline-flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-1.5 text-sm font-medium hover:bg-subtle"
          >
            {playing ? <Pause className="size-4" aria-hidden /> : <Play className="size-4" aria-hidden />}
            {playing ? 'Pause' : 'Play'}
          </button>
          <button
            type="button"
            onClick={() => {
              setStep(0);
              setPlaying(true);
            }}
            className="inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-sm text-muted hover:bg-subtle hover:text-fg"
          >
            <RotateCcw className="size-4" aria-hidden /> Restart
          </button>
          <span className="ml-auto text-xs text-muted">
            Step {step + 1} of {STEPS.length}
          </span>
        </div>
      </div>

      {/* phone + caption */}
      <div className="order-1 mx-auto flex flex-col items-center lg:order-2">
        <Phone>
          <div key={step} className="am-screen-in h-full">
            {current.screen()}
          </div>
        </Phone>
        <p className="mt-5 max-w-xs text-center text-sm text-muted" aria-live="polite">
          <strong className="text-fg">{current.title}.</strong> {current.caption}
        </p>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------ phone chrome

function Phone({ children }: { children: ReactNode }) {
  return (
    <div className="relative h-[540px] w-[264px] rounded-[2.6rem] border-[10px] border-neutral-900 bg-neutral-900 shadow-2xl dark:border-neutral-700">
      <div className="absolute top-2 left-1/2 z-20 h-4 w-20 -translate-x-1/2 rounded-full bg-neutral-900" aria-hidden />
      <div className="flex h-full flex-col overflow-hidden rounded-[2rem] bg-white text-[13px] text-neutral-900">
        <div className="flex h-8 shrink-0 items-center justify-between px-5 pt-1 text-[11px] font-medium">
          <span>9:41</span>
          <span className="flex items-center gap-1" aria-hidden>
            <Signal className="size-3" /> <Wifi className="size-3" /> <Battery className="size-3.5" />
          </span>
        </div>
        <div className="relative min-h-0 flex-1">{children}</div>
        <div className="flex h-5 shrink-0 items-center justify-center" aria-hidden>
          <span className="h-1 w-24 rounded-full bg-neutral-300" />
        </div>
      </div>
    </div>
  );
}

/** Pulsing ring that marks where the user taps. */
function Tap({ className }: { className: string }) {
  return <span className={cn('am-tap pointer-events-none absolute size-10 rounded-full bg-[#3949AB]/40 ring-2 ring-[#3949AB]', className)} aria-hidden />;
}

function AppBar({ title = 'AgentMesh', back = false }: { title?: string; back?: boolean }) {
  return (
    <div className="flex h-12 items-center gap-2 px-4 text-[17px] font-medium">
      {back && <ChevronLeft className="size-5" aria-hidden />}
      {title}
    </div>
  );
}

function Scrim({ children }: { children: ReactNode }) {
  return <div className="absolute inset-0 flex items-center justify-center bg-black/45 p-4">{children}</div>;
}

function Dialog({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="w-full rounded-3xl bg-[#f3f1fb] p-5 shadow-xl">
      <p className="text-[17px] font-medium">{title}</p>
      <div className="mt-3">{children}</div>
    </div>
  );
}

function PillButton({ children, outline = false, className }: { children: ReactNode; outline?: boolean; className?: string }) {
  return (
    <div
      className={cn(
        'flex h-10 items-center justify-center rounded-full px-4 text-[13px] font-medium',
        outline ? 'border border-neutral-400 text-[#3949AB]' : `${INDIGO} text-white`,
        className,
      )}
    >
      {children}
    </div>
  );
}

// ------------------------------------------------------------------ screens

function DownloadScreen() {
  return (
    <div className="flex h-full flex-col bg-neutral-50">
      <div className="mx-3 flex h-9 items-center rounded-full bg-neutral-200 px-3 text-[11px] text-neutral-600">github.com/…/agentmesh-control.apk</div>
      <div className="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center">
        <span className={cn('flex size-14 items-center justify-center rounded-2xl text-white', INDIGO)}>
          <ArrowDownToLine className="size-7" aria-hidden />
        </span>
        <p className="font-medium">Downloading…</p>
      </div>
      <div className="m-3 rounded-2xl bg-white p-3 shadow-md">
        <div className="flex items-center gap-2">
          <Download className="size-4 text-[#3949AB]" aria-hidden />
          <span className="flex-1 truncate text-[12px] font-medium">agentmesh-control.apk</span>
          <span className="text-[11px] text-neutral-500">36 MB</span>
        </div>
        <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-neutral-200">
          <div className={cn('am-download h-full rounded-full', INDIGO)} />
        </div>
        <div className="relative mt-3 flex justify-end">
          <span className="text-[12px] font-semibold text-[#3949AB]">Open</span>
          <Tap className="-top-2.5 -right-2" />
        </div>
      </div>
    </div>
  );
}

function AllowScreen() {
  return (
    <div className="flex h-full flex-col bg-white">
      <AppBar title="Install unknown apps" back />
      <div className="flex items-center gap-3 px-4 py-3">
        <span className="flex size-9 items-center justify-center rounded-full bg-neutral-100 text-[11px] font-bold">C</span>
        <div>
          <p className="font-medium">Chrome</p>
          <p className="text-[11px] text-neutral-500">135.0.7049</p>
        </div>
      </div>
      <div className="relative mx-4 flex items-center justify-between rounded-2xl bg-neutral-100 px-4 py-3">
        <span className="font-medium">Allow from this source</span>
        <span className={cn('flex h-6 w-11 items-center justify-end rounded-full p-0.5', INDIGO)} aria-hidden>
          <span className="size-5 rounded-full bg-white" />
        </span>
        <Tap className="top-0.5 right-1.5" />
      </div>
      <p className="px-4 pt-3 text-[11px] text-neutral-500">
        Your phone and personal data are more vulnerable to attack by unknown apps. Only allow apps you trust.
      </p>
    </div>
  );
}

function InstallScreen() {
  return (
    <div className="relative h-full bg-neutral-100">
      <Scrim>
        <Dialog title="AgentMesh">
          <div className="flex items-center gap-3">
            <AppIcon className="size-11" />
            <p className="text-[13px]">Do you want to install this app?</p>
          </div>
          <div className="relative mt-5 flex justify-end gap-5 text-[13px] font-semibold text-[#3949AB]">
            <span>Cancel</span>
            <span>Install</span>
            <Tap className="-top-3 -right-3" />
          </div>
        </Dialog>
      </Scrim>
    </div>
  );
}

function AppIcon({ className }: { className?: string }) {
  return (
    <span className={cn('flex items-center justify-center rounded-2xl', INDIGO, className)}>
      <svg viewBox="30 30 48 48" className="size-3/4" aria-hidden>
        <path d="M54,38 L38,66 M54,38 L70,66 M38,66 L70,66" className="stroke-white" strokeWidth={4} strokeLinecap="round" fill="none" />
        <circle cx={54} cy={38} r={8} className="fill-white" />
        <circle cx={38} cy={66} r={8} className="fill-white" />
        <circle cx={70} cy={66} r={8} className="fill-white" />
      </svg>
    </span>
  );
}

function WelcomeScreen() {
  return (
    <div className="flex h-full flex-col bg-white">
      <AppBar />
      <div className="mx-3 rounded-3xl bg-[#eef0fb] p-4">
        <p className="text-[16px] leading-snug font-semibold">Use another device's internet</p>
        <p className="mt-2 text-[11.5px] leading-relaxed text-neutral-600">
          Connect this phone to a computer running AgentMesh. Your internet will go out through that computer and websites will see its IP
          address.
        </p>
        <div className="relative mt-4">
          <PillButton className="h-11">Connect to remote server</PillButton>
          <Tap className="top-0.5 left-1/2 -translate-x-1/2" />
        </div>
        <p className="mt-3 text-[10.5px] text-neutral-500">You need the QR code or 6-digit code from the dashboard → Connect a phone.</p>
      </div>
      <p className="mt-auto pb-2 text-center text-[10px] text-neutral-400">AgentMesh v0.6.4</p>
    </div>
  );
}

function ConnectDialog({ code = '' }: { code?: string }) {
  return (
    <div className="relative h-full bg-white">
      <AppBar />
      <Scrim>
        <Dialog title="Connect to remote server">
          <div className="space-y-2.5">
            <PillButton className="gap-2">
              <QrCode className="size-4" aria-hidden /> Scan QR code
            </PillButton>
            <PillButton outline className="gap-2">
              <ImageIcon className="size-4" aria-hidden /> Choose QR code from gallery
            </PillButton>
            <p className="pt-1 text-[11px] text-neutral-600">or enter the 6-digit pairing code:</p>
            <div className="flex h-11 items-center rounded-lg border-2 border-[#3949AB] px-3 font-mono text-[20px] tracking-[0.3em]">
              {code || <span className="text-neutral-400">123 456</span>}
            </div>
          </div>
          <div className="mt-4 flex justify-end gap-4 text-[13px] font-semibold text-[#3949AB]">
            <span>Cancel</span>
            <span className={code.replace(' ', '').length === 6 ? '' : 'opacity-40'}>Connect</span>
          </div>
        </Dialog>
      </Scrim>
      {!code && <Tap className="top-[34%] left-1/2 -translate-x-1/2" />}
    </div>
  );
}

function ScanScreen() {
  return (
    <div className="relative flex h-full flex-col items-center justify-center bg-neutral-900 text-white">
      <p className="absolute top-4 px-6 text-center text-[11px] text-white/80">Scan the QR code from AgentMesh → Connect a phone</p>
      <div className="relative size-44 rounded-xl border-2 border-white/80 p-3">
        <QrArt className="size-full" />
        <span className="am-scan-line absolute inset-x-2 h-0.5 bg-emerald-400 shadow-[0_0_12px_2px] shadow-emerald-400" aria-hidden />
      </div>
      <p className="absolute bottom-6 rounded-full bg-white/15 px-3 py-1 text-[11px]">Hold steady…</p>
    </div>
  );
}

function GalleryScreen() {
  const tiles = Array.from({ length: 9 }, (_, i) => i);
  return (
    <div className="flex h-full flex-col bg-white">
      <AppBar title="Select a photo" back />
      <div className="grid grid-cols-3 gap-1 px-1">
        {tiles.map((i) => (
          <div
            key={i}
            className={cn(
              'relative flex aspect-square items-center justify-center rounded-sm',
              i === 0 ? 'bg-neutral-100 ring-4 ring-[#3949AB] ring-inset' : ['bg-amber-200', 'bg-sky-200', 'bg-emerald-200', 'bg-rose-200', 'bg-violet-200'][i % 5],
            )}
          >
            {i === 0 && (
              <>
                <QrArt className="size-14" />
                <Tap className="top-4 left-4" />
              </>
            )}
          </div>
        ))}
      </div>
      <p className="px-4 pt-3 text-[11px] text-neutral-500">Screenshot · Connect a phone (AgentMesh)</p>
    </div>
  );
}

function PairScreen() {
  const [typed, setTyped] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setTyped((n) => (n >= 6 ? n : n + 1)), 420);
    return () => clearInterval(t);
  }, []);
  const digits = '482913'.slice(0, typed);
  return <ConnectDialog code={digits.length > 3 ? `${digits.slice(0, 3)} ${digits.slice(3)}` : digits} />;
}

function VpnScreen() {
  return (
    <div className="relative h-full bg-white">
      <AppBar />
      <Scrim>
        <Dialog title="Connection request">
          <div className="flex gap-3">
            <ShieldCheck className="size-6 shrink-0 text-[#3949AB]" aria-hidden />
            <p className="text-[12px] leading-relaxed text-neutral-700">
              AgentMesh wants to set up a VPN connection that allows it to monitor network traffic. Only accept if you trust the source.
            </p>
          </div>
          <div className="relative mt-5 flex justify-end gap-5 text-[13px] font-semibold text-[#3949AB]">
            <span>Cancel</span>
            <span>OK</span>
            <Tap className="-top-3 -right-3.5" />
          </div>
        </Dialog>
      </Scrim>
    </div>
  );
}

function ConnectedScreen() {
  return (
    <div className="flex h-full flex-col bg-white">
      <AppBar />
      <div className="mx-3 rounded-3xl bg-[#e3f5e9] p-4">
        <p className="flex items-center gap-2 text-[14px] font-semibold">
          <span className="size-2 rounded-full bg-emerald-500" aria-hidden /> Connected — internet via Home computer
        </p>
        <p className="mt-1.5 text-[11.5px] text-neutral-600">↑ 2.1 MB   ↓ 48.6 MB</p>
        <p className="mt-2 text-[12.5px] font-semibold">Your IP now: 49.205.112.37</p>
        <div className="mt-3 flex gap-2">
          <PillButton outline className="h-9 flex-1 px-1 text-[11.5px] min-w-0 whitespace-nowrap">Check my IP</PillButton>
          <PillButton className="h-9 min-w-0 flex-1 px-1 text-[11.5px] whitespace-nowrap">Disconnect</PillButton>
        </div>
      </div>
      <p className="mx-4 mt-4 text-[12px] font-semibold">Saved servers</p>
      <div className="mx-3 mt-2 flex items-center justify-between rounded-2xl bg-[#eef0fb] px-4 py-3">
        <div>
          <p className="text-[13px] font-semibold">Home computer</p>
          <p className="text-[10.5px] text-neutral-500">Family link</p>
        </div>
        <span className="flex items-center gap-1 text-[12px] font-semibold text-emerald-600">
          <KeyRound className="size-3.5" aria-hidden /> Connected
        </span>
      </div>
      <div className="mx-3 mt-auto mb-2 flex items-center gap-2 rounded-xl bg-neutral-900 px-3 py-2 text-[11px] text-white">
        <ShieldCheck className="size-4 text-emerald-400" aria-hidden /> VPN on · AgentMesh
      </div>
    </div>
  );
}
