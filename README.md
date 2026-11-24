# tgbus

Interactive map of the TGSRTC (Telangana State Road Transport Corporation) GTFS feed.
Work in progress: a public web explorer and a bus journey planner are planned.

The GTFS feed is not in this repository; download it from Open Data Telangana
and keep it outside git (see `NOTICE` for attribution).

```sh
go run ./cmd/tgbus serve -feed Telangana_opendata_gtfs_TGSRTC_25_September_2026
# open http://localhost:8080

go run ./cmd/tgbus stats -feed Telangana_opendata_gtfs_TGSRTC_25_September_2026
```

Flags: `-feed <dir>` (GTFS folder, default as above), `-addr <host:port>` for `serve` (default `localhost:8080`).
Loading the full feed takes ~2 s. The page needs internet for the OpenStreetMap tiles, the MapLibre CDN and map fonts.

## What it shows
- **All routes**: every route line, faintly. Click a line to select that route.
- **Stops**: clustered; click a stop for the routes serving it and their trips per day.
- **Service intensity**: heat layer weighted by trips per stop.
- **Search**: by route number or place name; selecting a route draws it by direction with numbered stops, first/last departure and trips per day. Deep link with `#route=<route_id>`.

## API
| Endpoint | Returns |
|---|---|
| `GET /api/overview` | all routes, coarse geometry (gzip) |
| `GET /api/stops[?bbox=w,s,e,n]` | stop points with `trips`/`routes` counts |
| `GET /api/stops/{id}` | stop details and serving routes |
| `GET /api/routes?q=` | up to 50 matching routes |
| `GET /api/routes/{id}` | route lines and ordered stops (GeoJSON) |

## Feed quirks handled
- Quoted commas in names, non-standard `stop_times.txt` column order, times past `24:00:00`.
- 2 routes have no trips; some trips reference no shape (falls back to a line through the stops).
- 62 routes use their internal id as the route number; shown as "—".
- Shapes (3M points) are simplified with Douglas–Peucker (10 m for route detail, 50 m for the overview).

## Tests
`go test ./...` (table-driven; fixture feed in `testdata/feed`).
