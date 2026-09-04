# World Bank: people and prosperity

Chunk 2 adds five reviewed World Development Indicators to Atlas's existing owner preview,
confirmation, immutable release/snapshot and refresh flow. Public reads stay anonymous.

| Measure | Official indicator | Unit |
|---|---|---|
| Population | [SP.POP.TOTL](https://data.worldbank.org/indicator/SP.POP.TOTL) | People, midyear |
| Urban population | [SP.URB.TOTL.IN.ZS](https://data.worldbank.org/indicator/SP.URB.TOTL.IN.ZS) | Percent of population |
| GDP per capita, PPP | [NY.GDP.PCAP.PP.KD](https://data.worldbank.org/indicator/NY.GDP.PCAP.PP.KD) | Constant 2021 international dollars |
| Gini index | [SI.POV.GINI](https://data.worldbank.org/indicator/SI.POV.GINI) | Index, 0–100 |
| Unemployment | [SL.UEM.TOTL.ZS](https://data.worldbank.org/indicator/SL.UEM.TOTL.ZS) | Percent of labour force, modelled ILO estimate |

## Import

1. Log in as the owner and open `/admin`.
2. Choose one of the World Bank indicators. Create its staged preview.
3. Review row accounting and geography warnings, then confirm the immutable release.
4. Open `/countries` or `/explore`; choose an exact year and inspect the source details.
5. Repeat for the other measures. Confirmed indicators participate in the existing scheduled refresh.

Only those exact World Bank indicator-page URLs are accepted. The importer generates requests
to `api.worldbank.org`; arbitrary URLs, query strings, proxies, redirects and private DNS targets
are not accepted. Requests have a 45-second timeout, each import a five-minute deadline, and
the complete artifact is bounded to 50 MiB. Every response page is reconciled with the reported
total and source update; changing or incomplete pagination fails without publishing.

## Fidelity and interpretation

Each release stores a deterministic ZIP containing the **original bytes** of the indicator
metadata, country catalog and all observation JSON pages. No transformed CSV stands in for
the source. Per-file request URLs, media types, SHA-256 checksums, access times and available
ETag/Last-Modified headers are retained in release response metadata. The ZIP checksum excludes
access timestamps, so unchanged responses reuse the release. Confirmation reparses the ZIP.

The API exposes provider-neutral indicator fields alongside the older WHO fields, which remain
empty for World Bank data. Producer, source notes, provider update date and the reviewed CC BY 4.0
licence are attached to each release. Country-year footnotes and source flags appear in portraits,
the observation table and CSV. Charts and image exports identify their actual provider; full
producer citations and methodology are available in release details and CSV exports.

Country ISO3 codes are matched exactly against the pinned UN M49 reference. Source aggregates
remain separate. Unknown codes stay in source tables and exports; they are not guessed onto maps.
Population and urban definitions follow the source metadata. GDP is output, not household income.
Gini observations may use income or consumption surveys and have irregular years. Unemployment
is explicitly labelled as a modelled ILO estimate. Missing values and numeric zero stay distinct;
neither `gapfill` nor nearest-year substitution is used.

## Live verification, 2026-09-05

All five series were fetched and confirmed through the service's preview/confirmation flow in
an isolated PostgreSQL 18 test database. Each returned **17,490 rows**, spanning 1960–2025:
**87,450 original observation rows** in total, with zero rejected or collapsed rows. Many rows
are null; that is coverage information rather than evidence of a published value.

| Dataset | Numeric rows, including source aggregates |
|---|---:|
| Population | 17,394 |
| Urban population | 17,424 |
| GDP per capita, PPP | 8,669 |
| Gini | 2,430 |
| Unemployment | 8,176 |

The source contains 217 country/area codes and 48 aggregate codes. Of the country/area codes,
215 map to the current M49 reference. Kosovo (`XKX`) and Channel Islands (`CHI`) remain unmapped.
Mapping a country does not mean a numeric observation exists for every measure/year.

Reproduce the live check **only with an isolated database whose name contains `test`**:

```sh
ATLAS_WDI_LIVE=1 go test ./internal/atlas -run '^TestIntegrationWorldBankLive$' -v -count=1 -timeout=16m
```

Set `TEST_DATABASE_URL` before running. Like the existing integration helpers, this check resets
the test database. It leaves the five real WDI releases available afterward. Ordinary tests use
the explicitly synthetic ZIP in `internal/worldbank/testdata`, never that fixture in a real catalog.

The v2 [World Bank API documentation](https://datahelpdesk.worldbank.org/knowledgebase/articles/898581-api-basic-call-structures)
describes pagination, JSON output and `footnote=y`. The broader UN/agency roadmap is in
[the source research brief](data-source-research-2026-09-05.md).
