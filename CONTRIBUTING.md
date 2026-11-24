# Contributing

Thanks for helping make TGSRTC bus data easier to use.

## Setup
1. Install Go (see `go.mod` for the version).
2. Download the GTFS feed yourself (see the README) and keep it outside git; the
   repo ignores `Telangana_opendata_gtfs_*/`.
3. `go test ./...` runs against the small fixture feed in `testdata/feed`, so no
   download is needed for tests.

## Making a change
- Keep changes focused; open an issue first for anything large.
- Write Go tests as **table-driven tests** (a slice of cases run with `t.Run`).
- Run `go vet ./... && go test -race ./... && golangci-lint run` before opening a PR.
- Front-end code is plain ES modules with no build step; run `node --check` on JS you touch.
- Use clear commit messages in the imperative mood ("Add stop timetable").

## Data
Source data belongs to its publisher. Do not commit feed files or large derived
data. If something looks wrong in the feed itself, use the "Data issue" template.
