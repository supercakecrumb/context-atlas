# Versioned geographic reference assets

`un-m49-current.json` is a checked-in snapshot of the current UN M49 hierarchy.
Its regions, subregions, intermediate regions, LDC, LLDC, and SIDS membership are
labelled as the current classification applied across historical data. The custom
`custom:ex_soviet` group has the fifteen M49 members specified for Context Atlas.
Its `kind` values are API-ready: `world`, `region`, `subregion`,
`intermediate_region`, `ldc`, `lldc`, `sids`, and `custom`.

`natural-earth-admin0-50m.geojson` is a compact, pinned Natural Earth Admin-0
Countries 1:50m v5.1.1 snapshot, converted at acquisition time from Natural
Earth's official shapefile archive. It uses the M49/ISO crosswalk in the UN asset; unmatched
features and M49 areas are recorded under `geometry_join` rather than discarded.

Run `./assets/reference/update.sh` from the repository root to deliberately refresh
both snapshots. The service must only read these versioned artifacts; it never
downloads reference data at runtime. Natural Earth data is public domain: “Made
with Natural Earth.”
