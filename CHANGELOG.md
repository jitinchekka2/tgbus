# Changelog

All notable changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **Journey planner.** Directions between two points with transfers, computed in the browser
  (RAPTOR in Go compiled to WebAssembly, in a Web Worker): by stop name, your location or a
  point picked on the map; departure time; best journey per number of buses; step-by-step
  legs drawn on the map; shareable `#/plan/...` links; "Directions from/to here" on stop pages.
- `tgbus plan` to plan a journey from the command line; `tgbus build` now also writes
  `data/timetable.bin` (1.15 MB, 0.56 MB gzipped for the full feed).
- `timetable`, `raptor` and `planapi` packages; a Makefile (`make site`, `make serve`).
- Planner correctness tests against an independent Connection Scan implementation, including
  an opt-in run on the real feed (`TGBUS_FEED`), and a Node test that runs the WebAssembly build.

### Added
- `tgbus build` generates a fully static site (map data, search index, per-route and
  per-stop JSON) and `tgbus serve` previews it.
- Route pages and stop pages with shareable links; stop timetables with next departures
  in India time; accessible keyboard search over routes and stops.
- Light and dark themes (OpenFreeMap basemap), mobile layout, attribution footer,
  loading and error states.
- Public `gtfs`, `index` and `site` packages; front-end unit tests (`node --test`).

### Changed
- Replaced the prototype's Go HTTP API with static files.
- MapLibre GL JS is vendored (4.7.1) instead of loaded from a CDN.
