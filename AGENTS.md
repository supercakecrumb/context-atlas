# Context Atlas

## Product contract

Context Atlas is a public, English-only WHO and World Bank data explorer. The v0.1.0 release is a Go 1.26
modular monolith with PostgreSQL 18 and a React 19/Vite/Mantine frontend.

- Preserve every imported source row and immutable release/snapshot provenance.
- Year selection is exact. Never interpolate, substitute a nearest year, or present association
  as causation.
- Use established libraries for UI, icons, charts, maps, API contracts, auth, and database access.
  Never hand-draw SVG icons or create a homemade React/ECharts wrapper.
- Public reads are anonymous. Import, confirmation, and manual refresh are owner-only Telegram flows.
- World Bank imports accept only reviewed indicator URLs. Preserve original JSON responses, producer
  attribution, source notes and flags. Refresh new providers only after their first owner confirmation.
- Production releases use tagged GHCR images and Dokploy; follow the deploy skill for platform changes.

## Repository shape

- `cmd/context-atlas/` wires configuration, migrations, PostgreSQL, HTTP, refresh scheduling, and
  graceful shutdown.
- `internal/` owns domain packages; `migrations/` contains embedded, forward-only SQL migrations.
  Never rewrite an applied migration; source-neutral column names were introduced in `000002_sources.sql`.
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
