import { useState, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router';
import {
  Activity,
  Apple,
  ArrowRight,
  BellRing,
  Download,
  ExternalLink,
  Globe,
  KeyRound,
  Laptop,
  Lock,
  Monitor,
  Network,
  PlayCircle,
  QrCode,
  RefreshCw,
  ScrollText,
  Server,
  ShieldCheck,
  Smartphone,
  Users,
  Wifi,
  type LucideIcon,
} from 'lucide-react';
import { useAuth } from '@/auth/context';
import { ThemeToggle } from '@/app/ThemeToggle';
import { Button } from '@/components/ui/button';
import { CopyField } from '@/components/copy-button';
import { cn } from '@/lib/cn';
import { AndroidWalkthrough } from './AndroidWalkthrough';
import { QrArt } from './QrArt';

const REPO = 'https://github.com/devopshubtech/agentmeshvpn';
const RELEASES = 'https://github.com/devopshubtech/AgentMesh/releases/latest';
const APK = 'https://github.com/devopshubtech/AgentMesh/releases/latest/download/agentmesh-control.apk';
const MAC_PKG = 'https://github.com/devopshubtech/AgentMesh/releases/download/v0.6.4/agentmesh-server_0.6.4_macos_arm64.tar.gz';

/** Public product page: what AgentMesh does, how it works, how to install, and the demo. */
export function LandingPage() {
  const { status, startDemo } = useAuth();
  const navigate = useNavigate();
  const signedIn = status === 'authenticated';

  const openDemo = () => {
    startDemo();
    void navigate('/', { replace: true });
  };

  const demoButton = (label = 'Explore the live demo') => (
    <Button onClick={openDemo} icon={<PlayCircle className="size-4" aria-hidden />} className="h-11 px-5 text-base">
      {label}
    </Button>
  );

  return (
    <div className="min-h-full bg-bg text-fg">
      <SiteHeader signedIn={signedIn} onDemo={openDemo} />

      {/* ------------------------------------------------------------ hero */}
      <section className="mx-auto grid max-w-6xl items-center gap-10 px-4 pt-14 pb-16 md:grid-cols-2 md:pt-20">
        <div>
          <span className="inline-flex items-center gap-2 rounded-full border border-border bg-surface px-3 py-1 text-xs font-medium text-muted">
            <span className="size-1.5 rounded-full bg-emerald-500" aria-hidden />
            Self-hosted · open source · no router changes
          </span>
          <h1 className="mt-5 text-4xl leading-tight font-bold tracking-tight md:text-5xl">
            Use your computer's internet on your phone, <span className="text-primary">from anywhere.</span>
          </h1>
          <p className="mt-5 text-lg text-muted">
            AgentMesh turns a Mac, Windows or Linux computer into your private exit node. Phones connect with a QR code
            or a 6-digit code and browse with that computer's IP address, on mobile data or any Wi-Fi.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            {demoButton()}
            <a href="#install">
              <Button variant="outline" className="h-11 px-5 text-base" icon={<Download className="size-4" aria-hidden />}>
                Install AgentMesh
              </Button>
            </a>
          </div>
          <p className="mt-3 text-sm text-muted">The demo opens the real dashboard with sample devices. No sign-up.</p>
        </div>
        <HeroCard />
      </section>

      {/* ------------------------------------------------------------ use cases */}
      <Section id="why" eyebrow="Why AgentMesh" title="Your home or office connection, in your pocket">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Tile icon={Globe} title="Your IP, everywhere">
            Banking, streaming and work sites see your usual home or office IP, even when you travel.
          </Tile>
          <Tile icon={Laptop} title="Office allow-lists">
            Reach tools that only accept the office IP address, from a phone on mobile data.
          </Tile>
          <Tile icon={Users} title="Share with family">
            One QR code connects many phones. See who is connected and disconnect anyone.
          </Tile>
          <Tile icon={Activity} title="Test from anywhere">
            Check how your app or website looks from another network and another country.
          </Tile>
        </div>
      </Section>

      {/* ------------------------------------------------------------ how it works */}
      <Section id="how" eyebrow="How it works" title="Three parts, one private path">
        <FlowDiagram />
        <ol className="mt-10 grid gap-4 md:grid-cols-3">
          <Step n={1} title="Install the server">
            Run the AgentMesh server on the computer whose internet you want to use, for example a Mac mini at home. It
            gets a free public HTTPS address automatically, with no router or domain needed.
          </Step>
          <Step n={2} title="Create a QR code">
            In the dashboard, open <strong>Connect a phone</strong> and create a QR code or a 6-digit pairing code. Set an
            expiry, or keep it forever.
          </Step>
          <Step n={3} title="Connect the phone">
            Open the AgentMesh app, scan the code (or pick a screenshot from the gallery), and tap Connect. The phone's
            internet now goes out through your computer.
          </Step>
        </ol>
        <p className="mt-6 rounded-lg border border-border bg-surface p-4 text-sm text-muted">
          <strong className="text-fg">Under the hood:</strong> the phone app captures traffic with the system VPN, wraps every
          connection in an encrypted HTTPS WebSocket to the AgentMesh gateway, and the agent on your computer opens the real
          connection. Because everything travels as ordinary HTTPS, it works behind any router, on hotel Wi-Fi and on mobile
          networks. When the free address changes, the server publishes the new one and phones find it on their own.
        </p>
      </Section>

      {/* ------------------------------------------------------------ Android app tour */}
      <Section id="app-tour" eyebrow="Android app" title="See the Android app in action">
        <p className="-mt-4 mb-8 max-w-2xl text-muted">
          From download to connected in about a minute: install the app, open it, then scan the QR code, pick it from the gallery
          or type the pairing code.
        </p>
        <AndroidWalkthrough />
      </Section>

      {/* ------------------------------------------------------------ features */}
      <Section id="features" eyebrow="Features" title="Everything you need, nothing you don't">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Tile icon={QrCode} title="QR code or 6-digit code">
            Scan with the camera, choose the QR picture from the gallery, or type a short code that expires in 15 minutes.
          </Tile>
          <Tile icon={Wifi} title="Works on any network">
            Mobile data, hotel Wi-Fi, office networks. No port forwarding, no static IP, no domain to buy.
          </Tile>
          <Tile icon={RefreshCw} title="Follows address changes">
            The free tunnel address may change after a restart. Phones and the dashboard look up the new one automatically.
          </Tile>
          <Tile icon={BellRing} title="Telegram alerts">
            Get the server's new address on Telegram the moment it changes.
          </Tile>
          <Tile icon={Smartphone} title="See connected phones">
            Live list of phones per computer, with data used. Disconnect one phone or revoke a whole link instantly.
          </Tile>
          <Tile icon={Monitor} title="Manage your computers">
            Enroll Mac, Windows and Linux machines, see their health, approve new ones and run safe remote actions.
          </Tile>
          <Tile icon={Users} title="Team roles">
            Super admin, admin, operator and viewer roles, so each person sees only what they need.
          </Tile>
          <Tile icon={ScrollText} title="Tamper-evident audit log">
            Every sign-in, link, connection and command is recorded in a hash-chained log you can verify.
          </Tile>
          <Tile icon={Server} title="Self-hosted">
            Runs on your own Mac mini, PC or server. Your traffic never passes through a third-party VPN provider.
          </Tile>
        </div>
      </Section>

      {/* ------------------------------------------------------------ demo CTA */}
      <section className="mx-auto max-w-6xl px-4 py-6">
        <div className="flex flex-col items-start gap-5 rounded-2xl bg-primary px-6 py-8 text-primary-fg md:flex-row md:items-center md:justify-between md:px-10">
          <div>
            <h2 className="text-2xl font-bold">See the dashboard in action</h2>
            <p className="mt-1 opacity-90">
              Sample computers and phones, real screens. Create QR codes and pairing codes, explore every page.
            </p>
          </div>
          <Button variant="outline" onClick={openDemo} className="h-11 px-5 text-base" icon={<PlayCircle className="size-4" aria-hidden />}>
            Explore the demo, no login
          </Button>
        </div>
      </section>

      {/* ------------------------------------------------------------ install */}
      <Section id="install" eyebrow="Install" title="Up and running in about 10 minutes">
        <InstallTabs />
      </Section>

      {/* ------------------------------------------------------------ security */}
      <Section id="security" eyebrow="Security" title="Private by design">
        <div className="grid gap-4 md:grid-cols-3">
          <Tile icon={Lock} title="Encrypted end to end">
            Phone-to-server traffic is TLS, and websites' own HTTPS stays end to end. Connect keys are stored in the phone's
            secure key store.
          </Tile>
          <Tile icon={ShieldCheck} title="Owner stays in control">
            A computer only shares its internet if its owner enabled it at install. LAN addresses are refused by default.
          </Tile>
          <Tile icon={KeyRound} title="Revocable access">
            Links can expire or be revoked at any time; revoking disconnects its phones immediately. Sessions end after 12
            hours.
          </Tile>
        </div>
      </Section>

      {/* ------------------------------------------------------------ FAQ */}
      <Section id="faq" eyebrow="FAQ" title="Questions">
        <div className="grid gap-4 md:grid-cols-2">
          <Faq q="Is this a VPN?">
            For the phone, yes: it uses the phone's VPN feature. But instead of a commercial VPN server, your traffic goes out
            through your own computer, with its IP address.
          </Faq>
          <Faq q="How fast is it?">
            As fast as the computer's upload speed. Every byte the phone downloads is uploaded by your computer. On the free
            tunnel, expect around 10 Mbit/s; with your own domain or a server, it is much faster.
          </Faq>
          <Faq q="Does the computer have to stay on?">
            Yes. The phone uses that computer's internet, so it must be on and awake. On a Mac mini the server starts by
            itself after a restart.
          </Faq>
          <Faq q="Does it work on iPhone?">
            The Android app is available now. The iPhone app is built from the same code and is distributed through
            TestFlight.
          </Faq>
          <Faq q="What does it cost?">
            AgentMesh is free and self-hosted. The free Cloudflare tunnel and a free Neon database are enough for personal use.
          </Faq>
          <Faq q="Can I try it first?">
            Yes. <button type="button" onClick={openDemo} className="font-medium text-primary hover:underline">Open the demo</button>{' '}
            to use the real dashboard with sample data, no account needed.
          </Faq>
        </div>
      </Section>

      <footer className="mt-10 border-t border-border bg-surface">
        <div className="mx-auto flex max-w-6xl flex-col gap-4 px-4 py-8 text-sm text-muted md:flex-row md:items-center md:justify-between">
          <div className="flex items-center gap-2">
            <Logo />
            <span>AgentMesh: your computer's internet, on your phone.</span>
          </div>
          <div className="flex flex-wrap gap-x-5 gap-y-2">
            <a href={REPO} className="hover:text-fg">GitHub</a>
            <a href={RELEASES} className="hover:text-fg">Downloads</a>
            <button type="button" onClick={openDemo} className="hover:text-fg">Demo</button>
            <Link to="/login" className="hover:text-fg">Sign in</Link>
          </div>
        </div>
      </footer>
    </div>
  );
}

// ------------------------------------------------------------------ pieces

function Logo() {
  return (
    <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-fg">
      <Network className="size-4.5" aria-hidden />
    </span>
  );
}

function SiteHeader({ signedIn, onDemo }: { signedIn: boolean; onDemo: () => void }) {
  return (
    <header className="sticky top-0 z-30 border-b border-border bg-bg/85 backdrop-blur">
      <div className="mx-auto flex h-16 max-w-6xl items-center gap-4 px-4">
        <Link to="/welcome" className="flex items-center gap-2 font-semibold">
          <Logo />
          AgentMesh
        </Link>
        <nav className="ml-6 hidden gap-5 text-sm text-muted md:flex" aria-label="Sections">
          <a href="#how" className="hover:text-fg">How it works</a>
          <a href="#app-tour" className="hover:text-fg">App tour</a>
          <a href="#features" className="hover:text-fg">Features</a>
          <a href="#install" className="hover:text-fg">Install</a>
          <a href="#faq" className="hover:text-fg">FAQ</a>
        </nav>
        <div className="ml-auto flex items-center gap-2">
          <ThemeToggle />
          <a href={REPO} className="hidden items-center gap-1 rounded-md px-2 py-1.5 text-sm text-muted hover:bg-subtle hover:text-fg sm:flex">
            GitHub <ExternalLink className="size-3.5" aria-hidden />
          </a>
          {signedIn ? (
            <Link to="/">
              <Button size="sm">Open dashboard</Button>
            </Link>
          ) : (
            <>
              <Link to="/login" className="hidden sm:block">
                <Button size="sm" variant="ghost">Sign in</Button>
              </Link>
              <Button size="sm" onClick={onDemo}>Try the demo</Button>
            </>
          )}
        </div>
      </div>
    </header>
  );
}

function Section({ id, eyebrow, title, children }: { id: string; eyebrow: string; title: string; children: ReactNode }) {
  return (
    <section id={id} className="mx-auto max-w-6xl scroll-mt-20 px-4 py-14">
      <p className="text-sm font-semibold tracking-wide text-primary uppercase">{eyebrow}</p>
      <h2 className="mt-2 mb-8 text-3xl font-bold tracking-tight">{title}</h2>
      {children}
    </section>
  );
}

function Tile({ icon: Icon, title, children }: { icon: LucideIcon; title: string; children: ReactNode }) {
  return (
    <div className="rounded-xl border border-border bg-surface p-5">
      <span className="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
        <Icon className="size-5" aria-hidden />
      </span>
      <h3 className="mt-4 font-semibold">{title}</h3>
      <p className="mt-1.5 text-sm text-muted">{children}</p>
    </div>
  );
}

function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <li className="rounded-xl border border-border bg-surface p-5">
      <span className="flex size-8 items-center justify-center rounded-full bg-primary text-sm font-bold text-primary-fg">{n}</span>
      <h3 className="mt-4 font-semibold">{title}</h3>
      <p className="mt-1.5 text-sm text-muted">{children}</p>
    </li>
  );
}

function Faq({ q, children }: { q: string; children: ReactNode }) {
  return (
    <div className="rounded-xl border border-border bg-surface p-5">
      <h3 className="font-semibold">{q}</h3>
      <p className="mt-1.5 text-sm text-muted">{children}</p>
    </div>
  );
}

/** Mock phone + dashboard card for the hero. */
function HeroCard() {
  return (
    <div className="relative mx-auto w-full max-w-md pb-10">
      <div className="rounded-2xl border border-border bg-surface p-5 shadow-xl">
        <div className="flex items-center justify-between">
          <span className="text-sm font-semibold">Connect a phone</span>
          <span className="inline-flex items-center gap-1.5 text-xs text-muted">
            <span className="size-2 rounded-full bg-emerald-500" aria-hidden /> Office Mac mini · online
          </span>
        </div>
        <div className="mt-5 flex items-center gap-5">
          <QrArt className="size-32 shrink-0 border border-border" />
          <div className="min-w-0 text-sm">
            <p className="font-semibold">Scan with the phone</p>
            <p className="mt-1 text-muted">or type the pairing code</p>
            <p className="mt-3 font-mono text-2xl font-bold tracking-[0.25em] text-primary">482 913</p>
          </div>
        </div>
        <div className="mt-5 space-y-2 border-t border-border pt-4 text-sm">
          <p className="text-xs font-medium tracking-wide text-muted uppercase">Phones connected now</p>
          <PhoneRow name="Pixel 8" data="↓ 1.6 GB" />
          <PhoneRow name="Galaxy S23" data="↓ 120 MB" />
        </div>
      </div>
      <div className="absolute -bottom-1 -left-4 hidden rounded-xl border border-border bg-surface px-4 py-3 text-sm shadow-lg sm:block">
        <p className="text-xs text-muted">Websites now see</p>
        <p className="font-mono font-semibold">49.205.112.37 · Home</p>
      </div>
    </div>
  );
}

function PhoneRow({ name, data }: { name: string; data: string }) {
  return (
    <div className="flex items-center justify-between rounded-lg bg-subtle px-3 py-2">
      <span className="flex items-center gap-2">
        <Smartphone className="size-4 text-muted" aria-hidden />
        {name}
      </span>
      <span className="text-xs text-muted">{data}</span>
    </div>
  );
}

/** Phone → AgentMesh server → your computer → internet. */
function FlowDiagram() {
  const node = (icon: LucideIcon, title: string, sub: string) => {
    const Icon = icon;
    return (
      <div className="flex flex-1 flex-col items-center rounded-xl border border-border bg-surface p-5 text-center">
        <span className="flex size-11 items-center justify-center rounded-xl bg-primary/10 text-primary">
          <Icon className="size-6" aria-hidden />
        </span>
        <p className="mt-3 font-semibold">{title}</p>
        <p className="mt-1 text-xs text-muted">{sub}</p>
      </div>
    );
  };
  const arrow = (label: string) => (
    <div className="flex shrink-0 flex-col items-center justify-center px-1 py-2 text-xs text-muted md:w-28">
      <span className="md:hidden" aria-hidden>↓</span>
      <span className="hidden items-center gap-1 md:flex" aria-hidden>
        <span className="h-px w-10 bg-border" /> <ArrowRight className="size-4" />
      </span>
      <span className="mt-1 text-center">{label}</span>
    </div>
  );
  return (
    <div className="flex flex-col items-stretch md:flex-row">
      {node(Smartphone, 'Your phone', 'AgentMesh app, any network')}
      {arrow('encrypted HTTPS')}
      {node(Server, 'AgentMesh server', 'free public address, dashboard')}
      {arrow('relay')}
      {node(Monitor, 'Your computer', 'Mac, Windows or Linux')}
      {arrow('its own IP')}
      {node(Globe, 'The internet', 'sees your computer')}
    </div>
  );
}

// ------------------------------------------------------------------ install

type Tab = 'mac' | 'docker' | 'android' | 'iphone';

function InstallTabs() {
  const [tab, setTab] = useState<Tab>('mac');
  const tabs: { id: Tab; label: string; icon: LucideIcon }[] = [
    { id: 'mac', label: 'Mac mini server', icon: Apple },
    { id: 'docker', label: 'Windows / Linux server', icon: Monitor },
    { id: 'android', label: 'Android phone', icon: Smartphone },
    { id: 'iphone', label: 'iPhone', icon: Smartphone },
  ];
  return (
    <div className="rounded-2xl border border-border bg-surface">
      <div className="flex gap-1 overflow-x-auto border-b border-border p-2" role="tablist">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => setTab(t.id)}
            className={cn(
              'flex shrink-0 items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium',
              tab === t.id ? 'bg-primary/10 text-primary' : 'text-muted hover:bg-subtle hover:text-fg',
            )}
          >
            <t.icon className="size-4" aria-hidden />
            {t.label}
          </button>
        ))}
      </div>
      <div className="space-y-4 p-5 md:p-6">
        {tab === 'mac' && (
          <>
            <p className="text-sm text-muted">
              Recommended. Runs natively (no Docker), starts at boot, and makes the Mac the computer phones connect through.
              You need a free Postgres database, for example from Neon (use its <em>direct</em> connection string).
            </p>
            <Steps
              items={[
                <>Download the server package (Apple Silicon; Intel builds are on the downloads page).</>,
                <>Run the installer. It asks only for the database URL and your admin email and password.</>,
                <>Open the public address it prints, sign in, and go to <strong>Connect a phone</strong>.</>,
              ]}
            />
            <CopyField label="Terminal" value={`curl -fLO ${MAC_PKG}\ntar -xzf agentmesh-server_0.6.4_macos_arm64.tar.gz && cd agentmesh-server\n./install.sh`} />
            <p className="text-xs text-muted">
              Keep the Mac awake: System Settings → Energy → Prevent automatic sleeping. Re-run <code>./install.sh</code> to
              update; your settings are kept.
            </p>
          </>
        )}
        {tab === 'docker' && (
          <>
            <p className="text-sm text-muted">Runs the whole server in Docker Desktop, including its own database.</p>
            <Steps
              items={[
                <>Install Docker Desktop and Git.</>,
                <>Clone the project and generate keys and passwords (once).</>,
                <>Start everything: server, dashboard, public tunnel and this computer as the exit node.</>,
              ]}
            />
            <CopyField
              label="PowerShell (Windows)"
              value={`git clone ${REPO}.git\ncd agentmeshvpn\npowershell -ExecutionPolicy Bypass -File scripts\\dev-setup.ps1\npowershell -ExecutionPolicy Bypass -File scripts\\start-agentmesh.ps1`}
            />
            <CopyField
              label="Linux"
              value={`git clone ${REPO}.git && cd agentmeshvpn\nsh scripts/dev-setup.sh\ndocker compose -f infrastructure/docker/docker-compose.yml --profile public --profile exit up -d --build`}
            />
          </>
        )}
        {tab === 'android' && (
          <>
            <Steps
              items={[
                <>On the phone, download the app and allow "Install unknown apps" when asked.</>,
                <>Open AgentMesh → <strong>Connect to remote server</strong>.</>,
                <>Scan the QR code, choose a screenshot of it from the gallery, or type the 6-digit code. Tap Connect.</>,
              ]}
            />
            <a href={APK}>
              <Button icon={<Download className="size-4" aria-hidden />}>Download the Android app (APK)</Button>
            </a>
            <p className="text-xs text-muted">
              Android 8 or newer. Updates install over the old version.{' '}
              <a href="#app-tour" className="font-medium text-primary hover:underline">
                Watch the step-by-step walkthrough
              </a>
              .
            </p>
          </>
        )}
        {tab === 'iphone' && (
          <>
            <p className="text-sm text-muted">
              The iPhone app uses the same connect codes and tunnel engine as Android. iPhone apps install through Apple's{' '}
              <strong>TestFlight</strong>: install TestFlight from the App Store, open the invite link you received, then
              install AgentMesh.
            </p>
            <p className="text-sm text-muted">
              Building it yourself needs a Mac with Xcode and an Apple Developer account; see{' '}
              <a href={`${REPO}/tree/main/mobile/ios-control`} className="font-medium text-primary hover:underline">
                mobile/ios-control
              </a>
              .
            </p>
          </>
        )}
      </div>
    </div>
  );
}

function Steps({ items }: { items: ReactNode[] }) {
  return (
    <ol className="space-y-2 text-sm">
      {items.map((item, i) => (
        <li key={i} className="flex gap-3">
          <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-subtle text-xs font-semibold">{i + 1}</span>
          <span className="pt-0.5">{item}</span>
        </li>
      ))}
    </ol>
  );
}
