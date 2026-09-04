# Atlas data expansion: official sources

Researched 2026-09-05. This brief records the initial source selection. **Chunk 2 update:** the
five World Bank people/prosperity measures are implemented and were imported into an isolated
local preview database; see [implementation and live verification](worldbank-import.md).
Other organizations below remain research candidates. The original discovery findings and
pre-implementation requirements below are retained as dated context.

## Recommendation

Expand the health-focused launch catalog into five sections: **People, Prosperity, Education, Health, and Environment**. Start with a curated World Bank WDI adapter and a UN SDG adapter. Keep original agency attribution: the API distributor is often not the statistical producer. Add UNDP human-development data once its official download is accessible, and UN DESA population structure once the app can explicitly distinguish estimates from projections.

The immediate product is a country portrait with exact-year comparisons and historical trends. A later population pyramid, education breakdown, or migration flow should be a separate, clearly defined view rather than squeezing multidimensional data into a single country score.

## Delivery in five chunks

Aurora selected all themes, delivered incrementally. Each chunk ends with a usable result and the repository checks; source discovery alone does not count as an import.

| Chunk | Working result | Data scope and completion boundary |
|---|---|---|
| 1. Country portraits | `/countries`: a country, an optional comparison, exact-year headlines, historical charts, a location map, citations and a shareable snapshot | Existing WHO catalog. This research brief accompanies the feature; no additional provider is imported in this chunk. |
| 2. People and prosperity | Add the first non-WHO provider through the existing preview/confirmation flow | WDI population, urban population, GDP per capita PPP, Gini and modelled ILO unemployment. First complete the source-identity and original-artifact requirements below. Reuse an existing equivalent life-expectancy measure if present. |
| 3. Education and basic services | Add education, connectivity, electricity, water and sanitation with explicit population slices | UN SDG / UNESCO / UNICEF / WHO JMP / ITU, using one canonical series per measure. Confirm exact identifiers and coverage for sanitation before inclusion. |
| 4. Environment and energy | Compare air quality, emissions, renewable energy and food/land measures | UN SDG, FAO and IRENA. Keep energy capacity versus generation versus final consumption distinct; represent multi-year food-security periods truthfully before loading them. |
| 5. Migration, housing and wider quality of life | Add displacement/migration, housing, working time and human development where comparable data exist | UNHCR, UN DESA, OECD, Eurostat and UNDP. Keep migrant versus refugee concepts, estimates/projections and regional coverage explicit. Resolve download access and publish the actual coverage before promising worldwide cards. |

Health remains available throughout through WHO; child health and nutrition can be expanded with UNICEF/FAO in the relevant chunks. Detailed age/sex population structure belongs in chunk 5 once estimate/projection handling is ready. Every chunk retains missing values and provenance, and goes through the owner publication boundary before new observations reach a public snapshot.

## Sources and what they contribute

| Source | Useful additions | Access checked | Fit and limits |
|---|---|---|---|
| [UN Statistics Division: Global SDG Database](https://unstats.un.org/sdgs/dataportal) | Water, sanitation, electricity, internet, school completion, air pollution, hunger, energy | [Live API schema](https://unstats.un.org/SDGAPI/swagger/); current series list fetched successfully | 713 series returned in the live catalog, release `2026.Q2.G.02`. M49 geography codes fit Atlas. Every series still needs explicit unit, dimensions, statistical-status and methodology handling. Series count is not the indicator count. |
| [World Bank: World Development Indicators](https://datatopics.worldbank.org/world-development-indicators/) | Population, GDP per capita at PPP, inequality, employment, urbanisation, connectivity, basic services | [V2 API](https://datahelpdesk.worldbank.org/knowledgebase/articles/889392); metadata and actual observations fetched | No API key. Broad coverage through one distributor; retain the underlying UN, ILO, ITU, WHO/UNICEF, national and other producers. Some returned `unit` fields are empty despite units in the indicator name. |
| [UNDP: Human Development Reports](https://hdr.undp.org/data-center/documentation-and-downloads) | HDI, inequality-adjusted HDI, schooling, gender indices, planetary pressures | Official 1990–2023 time-series CSV and metadata workbook links found; CSV fetch returned HTTP 403 | API 2.0 requires subscription/key. A dated bulk CSV is preferable for a reproducible release. Download accessibility remains unverified; HDI is a published composite index, not a universal quality-of-life score. |
| [UN DESA: World Population Prospects](https://population.un.org/wpp/?os=0) | Population, age structure, fertility, life expectancy, population projections | Official bulk-download and API documentation located | WPP 2024 covers 237 countries/areas and 1950–2100. The [World Bank release note](https://datatopics.worldbank.org/world-development-indicators/release-note/publications/Population_Estimates_and_Projections_for_the_World_Bank.pdf) identifies 1950–2023 estimates and 2024–2100 projections for that revision. Preserve revision, variant, age, sex, and estimate/projection status. |
| [UNESCO Institute for Statistics](https://databrowser.uis.unesco.org/resources) | School completion, out-of-school rates, literacy, education spending, research | Official browser, Data API and [bulk service](https://databrowser.uis.unesco.org/resources/bulk) located | February 2026 release is listed. School level, sex, residence, wealth and observed/modelled status matter. Use SDG series first where equivalent, to avoid duplicate imports. |
| [UNICEF](https://data.unicef.org/sdmx-api-documentation/) and WHO/UNICEF JMP | Child mortality, nutrition, immunisation, drinking water and sanitation | Official SDMX REST documentation read | JSON exposes codes and structure. Documentation warns that CSV labels alone are not reliable identifiers. Dataset licence must be checked separately from the website-content licence. |
| [FAO / FAOSTAT](https://www.fao.org/FAOSTAT/) | Food insecurity, undernourishment, food supply, land use, agricultural emissions | Official catalog and developer-portal announcement located | Covers more than 245 countries/territories from 1961. Some food-security series are three-year periods, not annual observations. Retain flags, units and period definitions. |
| [UNHCR](https://www.unhcr.org/what-we-do/reports-and-publications/data-and-statistics/global-public-api) | Refugees, asylum applications, displacement, origin/destination flows | Official [Refugee Statistics API](https://api.unhcr.org/docs/refugee-statistics.html) located | Refugees, asylum seekers, internally displaced people and migrants are distinct concepts. Stocks and flows cannot be added indiscriminately. Better as a later migration/displacement section. |
| [OECD](https://www.oecd.org/en/data/insights/data-explainers/2024/09/api.html) and [Eurostat](https://ec.europa.eu/eurostat/web/user-guides/data-browser/api-data-access/api-introduction) | Housing, disposable income, working time, employment, education, regional detail | Official SDMX / statistics API documentation located | Useful depth for their covered countries; not a substitute for a worldwide baseline. Preserve harmonisation and coverage limits. |
| [IRENA](https://www.irena.org/Data/Downloads/Tools) | Renewable electricity capacity and generation, by technology | Official downloadable statistics tool located | Capacity, generated electricity and total final energy consumption are different measures. Choose the measure in the UI rather than calling all three “renewables”. |

## Concrete initial indicator set

Retain the six existing WHO datasets. Add the following in source-reviewed batches; the list below is a proposal, not a statement that these are already loaded.

| Section | Indicator | Source identifier | Meaning / selection |
|---|---|---|---|
| People | Population | WDI `SP.POP.TOTL` | Midyear, de facto population; metadata attributes UN WPP plus national/Eurostat/UN statistical sources. |
| People | Life expectancy at birth | WDI `SP.DYN.LE00.IN` | Years, total; use one canonical series rather than a second undifferentiated WHO copy. |
| People | Urban population | WDI `SP.URB.TOTL.IN.ZS` | Share of population; national urban definitions differ. |
| Prosperity | GDP per capita, PPP | WDI `NY.GDP.PCAP.PP.KD` | Constant 2021 international dollars; economic output, not take-home income. |
| Prosperity | Gini index | WDI `SI.POV.GINI` | Distribution inequality; household-survey and income/consumption basis must remain attached. Expect irregular years. |
| Prosperity | Unemployment | WDI `SL.UEM.TOTL.ZS` | Total labour force, modelled ILO estimate; explicitly label “modelled”. |
| Prosperity | Internet use | WDI `IT.NET.USER.ZS` | Percent of population; original source ITU. UN SDG `IT_USE_ii99` is an alternative route, not automatically a second independent measure. |
| Basic services | Electricity access | WDI `EG.ELC.ACCS.ZS` | Percent of population. |
| Basic services | Safely managed drinking water | WDI `SH.H2O.SMDW.ZS` **or** UN SDG `SH_H2O_SAFE` | Keep WHO/UNICEF JMP attribution; national total versus rural/urban is an explicit dimension. |
| Environment | Renewable final energy | WDI `EG.FEC.RNEW.ZS` **or** UN SDG `EG_FEC_RNEW` | Share of total final energy consumption, not just electricity. |
| Environment | Fine particulate air pollution | UN SDG `EN_ATM_PM25` | Population-weighted annual mean PM2.5; micrograms per cubic metre; select residence/location explicitly. |
| Education | School completion | UN SDG `SE_TOT_CPLR` | Select one education level and population slice; additional slices stay available in the explorer. |
| Human development | HDI and schooling components | UNDP HDR 2025 bulk time series | 1990–2023 release located; download blocked in this verification. Follow-up, not a dependency for the first two adapters. |

The WDI [GDP](https://data.worldbank.org/indicator/NY.GDP.PCAP.PP.KD), [Gini](https://data.worldbank.org/indicator/SI.POV.GINI), [internet](https://data.worldbank.org/indicator/IT.NET.USER.ZS), and [water](https://data.worldbank.org/indicator/SH.H2O.SMDW.ZS) pages publish source information and CC BY 4.0 licensing. Check each selected dataset's metadata; do not infer blanket permission for every third-party series from the distributor's general terms.

## What was verified with live data

Successfully retrieved WDI data for Portugal and Sweden, ten proposed indicators, and 2020–2025: **120 country–indicator–year rows**, including nulls. Response metadata reported `lastupdated: 2026-07-13`.

| Series | Latest non-null Portugal year in this sample | Latest non-null Sweden year in this sample |
|---|---:|---:|
| Population | 2025 | 2025 |
| GDP per capita PPP | 2025 | 2025 |
| Life expectancy | 2024 | 2024 |
| Internet use | 2024 | 2025 |
| Safely managed drinking water | 2024 | 2024 |
| Gini | 2023 | 2023 |
| Renewable final energy | 2021 | 2021 |

This is a two-country integration probe, **not a worldwide coverage audit**. It demonstrates why “latest” cannot mean that every card shows the same year, and why an exact 2024 portrait must leave some values empty. API update dates and observation years are separate fields.

Reproducible calls:

```text
GET https://api.worldbank.org/v2/indicator/SP.POP.TOTL?format=json
GET https://api.worldbank.org/v2/country/PRT;SWE/indicator/NY.GDP.PCAP.PP.KD?source=2&format=json&date=2020:2025&footnote=y
GET https://unstats.un.org/SDGAPI/v1/sdg/Series/List?allreleases=false
GET https://unstats.un.org/SDGAPI/swagger/v1/swagger.json
```

The UN API schema confirms that `areaCode` takes M49 codes, `releaseCode` selects a release, `dimensions` filters slices, and `page`/`pageSize` paginate observations. Fetch all pages and reconcile the reported count. Do not use World Bank `gapfill=Y` or silently substitute its `mrnev` values for a selected exact year.

## Initial Atlas requirements, before the chunk 2 implementation

The World Bank implementation now addresses these requirements for its five reviewed indicators.
They remain acceptance criteria for subsequent providers; references to WHO-only columns below
describe the source state at the beginning of this research.

1. **Preserve source identity.** The current schema and API use `who_indicator_id`, `who_measure_code`, `who_code` and `who_identifier`; `insertPreviewFacts` hardcodes source system `WHO`, and chart/export attribution is also hardcoded. Add source-aware identity and presentation coherently before another provider is inserted. Do not label World Bank or UNDP data as WHO.
2. **Preserve original artifacts.** WDI and UN API responses are JSON; store their original bytes, content type, query, metadata, checksums, access time and parser version. A transformed CSV alone is not the original release. Existing raw artifact columns and WHO URL checks need an explicit extension, not bypassing validation.
3. **Retain the full meaning of a series.** Include producer, distributor, indicator code, unit/base year, age, sex, residence, education level, model/estimate/projection status and source notes. Unknown geographic codes remain unmapped; map ISO3 to the pinned M49 reference, never by approximate name. Retain aggregates separately from countries.
4. **Use the existing publication boundary.** Preview, account for every row, confirm as owner, then publish one immutable snapshot. Bound downloads, validate a provider allowlist, handle pagination and nulls, and refresh only supported sources. Public pages keep reading snapshots instead of calling upstream APIs from the browser.
5. **Verify the result.** A source-specific parser check plus integration coverage for original-artifact preservation, source attribution, missing versus zero, unknown geography, dimensions, exact-year comparisons and refresh idempotency. Follow the repository's full gate before release-ready status.

Country portraits are useful before this expansion: they show every numeric time-series dataset in the current catalog, preserve selected years, expose the published tuple, and link to the existing explorer for other slices. Composition datasets remain in the full catalog.
