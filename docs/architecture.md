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

## Journey planner

```
index.Index ──timetable.Build──▶ Timetable ──Encode──▶ data/timetable.bin
                                      │                       │ (fetched by the page)
                                      ▼                       ▼
                               raptor.Network  ◀── planapi ◀── planner.wasm (Web Worker)
```

- **timetable** groups trips into *patterns* (same route, same stops, same order). Trips of a
  pattern mostly share their run times, so a pattern stores a few *profiles* and each trip only
  its first departure plus a profile number. The full Telangana feed encodes to 1.15 MB
  (0.56 MB gzipped). `Decode` returns errors, never panics, on truncated or corrupt input
  (tested for every truncation length and thousands of random corruptions).
- **raptor** is round-based: round *k* finds the earliest arrival at every stop with at most
  *k* bus rides, which directly yields the best journey for each number of rides. Decisions
  specific to this feed:
  - *Non-overtaking classes.* RAPTOR assumes a later trip never reaches a stop before an
    earlier one on the same pattern. 393 real patterns break that, so each pattern is split
    into classes where it holds (4,125 patterns become 4,698 classes).
  - *Midnight.* The service runs every day, so a query is a time of day. Trips that run past
    midnight are also available at their wall-clock time the next morning (a copy shifted by
    -24 h), and tomorrow's trips (+24 h) serve late-night queries.
  - *Two arrival times per stop.* A rider who arrives by bus may walk on to another stop; one
    who arrived by walking may not. Keeping a bus arrival and a walk arrival separate matters:
    a single "best arrival" wrongly discarded bus arrivals that were later than an earlier walk.
  - *Transfers* have a 2-minute buffer after a bus. Origins and destinations are points: they
    connect to nearby stops on foot, and a walk-only option is offered for short trips.
- **planapi** validates requests (all fields required) and shapes responses as JSON so the
  WebAssembly wrapper stays a few lines long.
- **cmd/planner-wasm** exposes `load`, `plan` and `counts` to JavaScript. `web/js/planner.js`
  starts it in a Web Worker only when Directions is first opened, so the explorer's first
  load does not pay for it.

### How the planner is verified
The tests compare RAPTOR with **Connection Scan**, a different algorithm that shares none of
its machinery (no rounds, patterns or classes): about 5,500 random queries over 400 random
networks that include overtaking, loops, past-midnight trips and walking transfers must
produce identical earliest arrivals, and every returned journey is checked for continuity,
boarding times, transfer buffers and that each ride matches a real trip. The same comparison
runs on the real feed when `TGBUS_FEED` points at it (300 random queries, all identical).
Breaking each feature on purpose (no overtaking split, no midnight copies, no walking, no
buffer, the single-arrival pruning) makes these tests fail.

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
