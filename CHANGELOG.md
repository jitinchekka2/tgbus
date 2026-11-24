# Changelog

All notable changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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
