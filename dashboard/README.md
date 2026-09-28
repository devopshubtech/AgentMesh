# AgentMesh dashboard

React 19 + TypeScript + Vite admin console for AgentMesh. It talks only to `control-api`
(REST contract: `../docs/api.md`).

## Development

```sh
npm install
npm run dev        # http://localhost:13000, proxies /v1 -> http://localhost:18080 (control-api)
```

## Scripts

| Command             | What it does                                  |
| ------------------- | --------------------------------------------- |
| `npm run dev`       | Vite dev server on port 13000 (strict port)   |
| `npm run build`     | Type-check (`tsc -b`) and build into `dist/`  |
| `npm run typecheck` | Type-check only                               |
| `npm run lint`      | ESLint (typescript-eslint + react-hooks)      |
| `npm test`          | Vitest unit tests (format, argv, SSE parser)  |
| `npm run preview`   | Serve the production build on port 13000      |

## Container

```sh
docker build -t agentmesh-dashboard .
```

nginx listens on port 80 in the container, serves the SPA with a history fallback, and proxies
`/v1/` to `http://control-api:8080` (SSE on `/v1/events` is unbuffered). It sends a strict CSP
with no inline scripts or styles.

## Security notes

- The access token is kept **in memory only**. The session comes back through the HttpOnly
  `am_refresh` cookie (`POST /v1/auth/refresh` with `X-Requested-With: agentmesh`).
- Hiding UI based on permissions is only for usability. The API enforces authorization.
- Device-reported data is always rendered as text. `dangerouslySetInnerHTML` is banned by a lint rule.
