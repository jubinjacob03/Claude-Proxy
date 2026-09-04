# Claude-Proxy Licensing Panel

Admin site for the relay: mint licences, manage the pooled API keys, and watch
usage. See [PLAN.md](PLAN.md) for the full architecture.

Built with Next.js 16 / React 19, styled with the same theme as KeyAuth's panel
(components under `components/` and the theme in `app/globals.css` are copied
from `d:/My-Projects/KeyAuth` and adapted for a single-admin, single-product
panel with no user/reseller store).

## Setup

```powershell
npm install
Copy-Item .env.example .env
npm run hash-password -- "your-chosen-password"   # paste the output into ADMIN_PASSWORD_HASH
npm run build
npm run start
```

Required env vars (see `.env.example`):

| Variable                             | Purpose                                     |
| ------------------------------------ | ------------------------------------------- |
| `RELAY_BASE_URL`                     | Base URL of the running `claude-relay`      |
| `RELAY_ADMIN_TOKEN`                  | Must match `RELAY_ADMIN_TOKEN` on the relay |
| `ADMIN_USER` / `ADMIN_PASSWORD_HASH` | The one admin account                       |
| `SESSION_SECRET`                     | HMAC key for the login session cookie       |

## What each page does

- `/login` — admin sign-in
- `/dashboard` — totals: licences, activated machines, spend, pool credit
- `/licenses` — mint licences (key shown once), pause/resume, reset HWID, top up
- `/pool` — add pooled upstream keys with a dollar balance, enable/disable/delete
- `/usage` — recent metered requests per licence

## Security notes

- The relay admin token never reaches the browser: every relay call happens
  inside a Server Action or Server Component, both of which run only on this
  server (see `lib/relay.js`, which imports `server-only`).
- Licence keys are shown exactly once, right after minting, and never again.
- The login session cookie is HMAC-signed and `httpOnly`; there is no
  client-side auth state to tamper with.
- `TRUST_PROXY=true` (default in `.env.example` and docker-compose) is required
  behind the bundled Caddy reverse proxy, so rate limiting sees the real client
  IP from `X-Forwarded-For` instead of collapsing everyone into one bucket.

## Verified against the live relay

The admin API contract (`/admin/licenses`, `/admin/pool`, `/admin/usage`) was
checked field-by-field against a running `claude-relay` instance during
development; `lib/relay.js` and the page components consume those responses
directly with no field renamed on either side.
