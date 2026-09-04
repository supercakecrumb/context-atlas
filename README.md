# Context Atlas

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

Deployment is intentionally outside the v0.1.0 website-build goal. The planned public host is
`https://atlas.aurorass.art`.
