# Context Atlas

## Product contract

Context Atlas is a public, English-only WHO data explorer. The v0.1.0 release is a Go 1.26
modular monolith with PostgreSQL 18 and a React 19/Vite/Mantine frontend.

- Preserve every imported source row and immutable release/snapshot provenance.
- Year selection is exact. Never interpolate, substitute a nearest year, or present association
  as causation.
- Use established libraries for UI, icons, charts, maps, API contracts, auth, and database access.
  Never hand-draw SVG icons or create a homemade React/ECharts wrapper.
- Public reads are anonymous. WHO import, confirmation, and refresh are owner-only Telegram flows.
- Deployment and production infrastructure are outside the current v0.1.0 website goal.

## Repository shape

- `cmd/context-atlas/` wires configuration, migrations, PostgreSQL, HTTP, refresh scheduling, and
  graceful shutdown.
- `internal/` owns domain packages; `migrations/000001_init.sql` is the only pre-release migration.
- `web/` is the Vite app. Huma owns OpenAPI; Orval-generated frontend code lives only under
  `web/src/api/generated/`.
- `assets/` contains versioned source/reference assets with provenance and checksums, never secrets.

## Safety and verification

- Never commit or print secrets, raw production data, `.env`, dumps, logs, bot tokens, or session
  keys. `.env.example` contains placeholders only.
- Integration tests may reset only a database whose name contains `test`.
- Run `./scripts/pre-commit.sh` before release-ready status. It is the single full gate.
- Treat existing changes as owner work. Never reset, clean, or overwrite unrelated files.
- Commit as `Aurora Kiel <supercakecrumb@gmail.com>` without AI attribution or co-author trailers.

