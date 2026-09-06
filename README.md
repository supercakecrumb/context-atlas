# Context Atlas

[![CI](https://github.com/supercakecrumb/context-atlas/actions/workflows/ci.yml/badge.svg)](https://github.com/supercakecrumb/context-atlas/actions/workflows/ci.yml)
[![Release](https://github.com/supercakecrumb/context-atlas/actions/workflows/release.yml/badge.svg)](https://github.com/supercakecrumb/context-atlas/actions/workflows/release.yml)

[Public atlas](https://atlas.aurorass.art) · [MIT license](LICENSE) · [Container images](https://github.com/supercakecrumb/context-atlas/pkgs/container/context-atlas)

Context Atlas is an English-only public data atlas for exploring WHO and World Bank indicators across countries,
areas, years, maps, and exact-year associations.

The v0.1.0 launch catalog contains suicide mortality, alcohol consumption, tobacco prevalence,
homicide mortality, AWaRe antibiotic consumption, and road-traffic mortality. Source rows remain
attached to immutable releases and reproducible catalog snapshots.

Country portraits at `/countries` bring the catalog's numeric time series together for one
country and an optional comparison. Headline values use the selected exact year; historical
trends, published dimensions, and source details remain visible. The URL pins the catalog
snapshot and the selections.

The [official-source expansion brief](docs/data-source-research-2026-09-05.md) compares UN,
World Bank, UNDP and specialist agencies, with verified API samples and a proposed initial
indicator set. The first five World Bank measures are now supported: population, urban population,
GDP per capita at PPP, Gini and modelled ILO unemployment. Other agencies remain research candidates.

In the owner admin page, choose a World Bank indicator, create a preview, review its accounting
and warnings, then confirm the release. Confirmed indicators join country portraits, the explorer,
maps, associations and scheduled refreshes. See [World Bank import details](docs/worldbank-import.md).

## Local development

Requirements: Go 1.26, Node 24, npm 11, and PostgreSQL 18.

```bash
docker compose up -d db
make dev
```

Copy `.env.example` to an untracked `.env` only when local secrets are needed. Never commit it.

## Verification

```bash
./scripts/pre-commit.sh
```

## Data and attribution

WHO data is used under each dataset's stated terms. The reviewed World Bank WDI series use CC BY 4.0,
with original statistical producers and country-year source notes retained. Context Atlas identifies
every source release, access date and citation and does not imply agency endorsement. Map geometry is made with Natural
Earth public-domain data; UN M49 supplies the reference grouping hierarchy.

## Deployment

GitHub Actions checks Go, PostgreSQL integration, frontend tests, browser tests and API contracts.
Pushing a `vX.Y.Z` tag runs those checks again, publishes a Linux amd64 image to
`ghcr.io/supercakecrumb/context-atlas:X.Y.Z`, and creates a GitHub release. Deployment uses
`scripts/deploy.sh X.Y.Z` from the operator's machine, keeping the Dokploy API credential out
of GitHub. The script selects that exact image, waits for the new deployment and checks the public health endpoint.

The production host is `https://atlas.aurorass.art`. The container runs as a non-root user,
applies embedded migrations at startup, and serves the API and frontend on port 8080.
PostgreSQL stores source releases and snapshots. Configuration is documented in `.env.example`;
production credentials belong in OpenBao and Dokploy, never in Git.

For another Dokploy installation, supply `DOKPLOY_URL`, `DOKPLOY_API_TOKEN`, `DOKPLOY_APP_ID`,
and `HEALTH_URL=https://your-host/health` in the operator's environment. The script checks
`DATABASE_URL` is configured; `REQUIRE_ENV` can name additional required variables.
Make the GHCR package public before the first deployment.
The MIT license covers application code; source datasets retain their own attribution and terms.
