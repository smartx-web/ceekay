# CEEKAY Volunteers Portal — Abia State

Go/Gin + PostgreSQL volunteer registration portal adapted from DOV for Hon. Ceekay Igara (LP).

## Implemented
- Responsive volunteer application, administrator page and REST API.
- All 17 Abia LGAs seeded automatically.
- Ward and polling-unit tables deliberately empty pending a verified INEC Abia dataset.
- Registration is **off by default**; no volunteer submissions accepted until enabled.

## Local setup
Install Go 1.26 and PostgreSQL. Copy `.env.example` to `.env`, create a dedicated database, set `DATABASE_URL` and run:
```sh
set -a; . ./.env; set +a
go run ./cmd/server
```
Open `http://localhost:8080`; health endpoint: `/api/health`.

## Render
`render.yaml` configures a web service. Provision a separate PostgreSQL database and supply `DATABASE_URL`, `ADMIN_EMAIL`, `ADMIN_PASSWORD_HASH` via Render secrets. Never commit secrets.

## Security and launch checklist
The inherited HTTP Basic admin authentication is **not production ready**. Before public launch: implement secure sessions, HTTPS, CSRF protection, rate limiting, audit logs, backups, privacy notice and retention policy, verify INEC wards and polling units, test deployment and keep `REGISTRATION_ENABLED=false` until approval.

This is an independent volunteer portal, not an INEC registration or accreditation system. No live deployment or end-to-end verification has been performed.
