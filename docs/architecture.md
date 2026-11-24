# Architecture

```
GTFS feed ──tgbus build──▶ dist/
                            ├─ index.html, style.css, js/, vendor/   (web assets, copied)
                            └─ data/
                               ├─ meta.json          counts, feed name, build date
                               ├─ overview.json      every route, coarse geometry (network layer)
                               ├─ stops.json         every stop as a point (clusters, heat layer)
                               ├─ search.json        [id, number, long name, trips] per route
                               ├─ routes/<id>.json   one route: lines per direction + ordered stops
                               └─ stops/<id>.json    one stop: routes and departure minutes
```

## Packages
- **`gtfs`** reads CSV by header name through `encoding/csv`, so quoted commas and
  non-standard column order are fine. `stop_times.txt` (~60 MB) is streamed, never held as rows.
- **`index`** loads the feed and builds routes, one *line* per stopping pattern (a shape plus the
  stops of the longest trip using it), per-stop trip counts and per-stop departures. Rows that
  cannot be used are skipped and counted in `Stats.Skipped`; `tgbus stats` prints them.
- **`site`** writes the files above. Ids become file names, so `Build` rejects unsafe ids and ids
  that differ only by letter case (they would collide on case-insensitive file systems).
- **`web`** is embedded with `go:embed`, so one binary can build the whole site.

## Front end
Plain ES modules, no build step. The URL hash is the single source of truth for what the
panel shows (`#/route/<id>`, `#/stop/<id>`); ids from the hash are validated before they are
used in a fetch path. Pure logic (search ranking, next-departure maths, hash parsing) lives in
`web/js/util.js` so it can be unit-tested with `node --test`.

## Feed facts that shape the design
- A single all-days service, so queries are time-of-day only and there is no calendar logic.
- Arrival equals departure at every stop; the last stop of a trip is an arrival, so it is not
  listed as a departure.
- Times can pass 24:00 (for example 25:10); the UI shows the wall-clock time (01:10).
- No `transfers.txt` and no street network: relevant for the planned journey planner.
- Some routes carry their internal id as the route number; the UI shows a dash instead.

## Basemap
Configured in `web/js/config.js` (OpenFreeMap vector styles for light and dark). If the style
cannot be fetched, the page falls back to a plain background so routes and stops still show.
