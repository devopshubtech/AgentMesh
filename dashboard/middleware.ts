// Vercel Routing Middleware: forwards /v1/* to the AgentMesh backend.
//
// The backend runs behind a free Cloudflare quick tunnel whose address changes
// on every restart, so it is not hard-coded: the current address is read from
// the rendezvous gist that the server keeps up to date (the same one the
// Android app uses). The browser only ever talks to this origin, so the
// SameSite=Strict login cookie keeps working.
//
// Vercel environment variables (all optional):
//   AGENTMESH_BACKEND_URL     fixed backend URL; skips the gist (e.g. a named tunnel)
//   AGENTMESH_RENDEZVOUS_URL  gist API URL (default: the app's built-in gist)
//   AGENTMESH_GITHUB_TOKEN    raises GitHub's API rate limit for the lookup

export const config = { matcher: '/v1/:path*' };

const DEFAULT_RENDEZVOUS = 'https://api.github.com/gists/9b876c950f541c735c8a817c96362ca9';
const CACHE_MS = 30_000;

let cached: { url: string; at: number } | null = null;

async function lookup(): Promise<string | null> {
  const env = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {};
  if (env.AGENTMESH_BACKEND_URL) return env.AGENTMESH_BACKEND_URL;
  if (cached && Date.now() - cached.at < CACHE_MS) return cached.url;

  const headers: Record<string, string> = {
    Accept: 'application/vnd.github+json',
    'User-Agent': 'agentmesh-dashboard',
  };
  if (env.AGENTMESH_GITHUB_TOKEN) headers.Authorization = `Bearer ${env.AGENTMESH_GITHUB_TOKEN}`;
  try {
    const res = await fetch(env.AGENTMESH_RENDEZVOUS_URL || DEFAULT_RENDEZVOUS, { headers, cache: 'no-store' });
    if (res.ok) {
      const gist = (await res.json()) as {
        files?: Record<string, { content?: string }>;
      };
      const content = gist.files?.['agentmesh-endpoint.json']?.content;
      const url = content ? (JSON.parse(content) as { url?: string }).url : undefined;
      if (url && url.startsWith('https://')) {
        cached = { url: url.replace(/\/+$/, ''), at: Date.now() };
        return cached.url;
      }
    }
  } catch {
    // fall through to the last known address
  }
  return cached?.url ?? null;
}

export default async function middleware(request: Request): Promise<Response> {
  const backend = await lookup();
  if (!backend) {
    return Response.json(
      {
        error: {
          code: 'backend_unavailable',
          message: 'The AgentMesh server address is not known yet. Is the server running?',
        },
      },
      { status: 503 },
    );
  }
  const src = new URL(request.url);
  // Same contract as @vercel/functions rewrite(): Vercel proxies the request to this URL.
  return new Response(null, {
    headers: {
      'x-middleware-rewrite': `${backend}${src.pathname}${src.search}`,
    },
  });
}
