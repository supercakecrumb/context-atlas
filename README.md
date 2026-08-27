# Context Atlas

Context Atlas is an English-only public data atlas for exploring WHO indicators across countries,
areas, years, maps, and exact-year associations.

The v0.1.0 launch catalog contains suicide mortality, alcohol consumption, tobacco prevalence,
homicide mortality, AWaRe antibiotic consumption, and road-traffic mortality. Source rows remain
attached to immutable releases and reproducible catalog snapshots.

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

WHO data is used under each dataset's stated terms. Context Atlas identifies every source release,
access date, and citation and does not imply WHO endorsement. Map geometry is made with Natural
Earth public-domain data; UN M49 supplies the reference grouping hierarchy.

## Deployment

Deployment is intentionally outside the v0.1.0 website-build goal. The planned public host is
`https://atlas.aurorass.art`.
