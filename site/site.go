// Package site renders an index.Index into a static website: the web assets
// plus JSON data files that the page fetches on demand. The output needs no
// server code and uses only relative URLs, so it can be hosted from any path
// (for example GitHub Pages project sites).
package site

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jitinchekka2/tgbus/index"
	"github.com/jitinchekka2/tgbus/timetable"
)

// Options configure Build.
type Options struct {
	Out      string    // output directory
	Assets   fs.FS     // web assets copied to the output root (may be nil)
	FeedName string    // human-readable feed label shown on the site
	Now      time.Time // build time recorded in meta.json (zero means time.Now)
}

// Report summarises what Build wrote.
type Report struct {
	Files int
	Bytes int64
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// checkIDs ensures every id can be used as a file name on any file system:
// only safe characters, and no two ids that differ only by letter case.
func checkIDs(kind string, ids []string) error {
	seen := make(map[string]string, len(ids))
	for _, id := range ids {
		if !safeID.MatchString(id) {
			return fmt.Errorf("%s id %q contains characters that are unsafe in a file name", kind, id)
		}
		key := strings.ToLower(id)
		if prev, dup := seen[key]; dup {
			return fmt.Errorf("%s ids %q and %q differ only by case and would collide on case-insensitive file systems", kind, prev, id)
		}
		seen[key] = id
	}
	return nil
}

type writer struct {
	root string
	rep  Report
}

func (w *writer) writeFile(rel string, data []byte) error {
	full := filepath.Join(w.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return err
	}
	w.rep.Files++
	w.rep.Bytes += int64(len(data))
	return nil
}

func (w *writer) writeJSON(rel string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", rel, err)
	}
	return w.writeFile(rel, data)
}

// Build writes the static site for idx into opts.Out. Existing files under
// <out>/data are removed first so stale routes from an older feed cannot linger.
func Build(idx *index.Index, opts Options) (Report, error) {
	if opts.Out == "" {
		return Report{}, errors.New("output directory is required")
	}
	routeIDs := make([]string, len(idx.Routes))
	for i, r := range idx.Routes {
		routeIDs[i] = r.ID
	}
	stopIDs := make([]string, len(idx.Stops))
	for i, s := range idx.Stops {
		stopIDs[i] = s.ID
	}
	if err := checkIDs("route", routeIDs); err != nil {
		return Report{}, err
	}
	if err := checkIDs("stop", stopIDs); err != nil {
		return Report{}, err
	}

	if err := os.RemoveAll(filepath.Join(opts.Out, "data")); err != nil {
		return Report{}, err
	}
	w := &writer{root: opts.Out}

	if opts.Assets != nil {
		err := fs.WalkDir(opts.Assets, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if path.Base(p) == "embed.go" {
				return nil // Go source that lives beside the assets
			}
			b, err := fs.ReadFile(opts.Assets, p)
			if err != nil {
				return err
			}
			return w.writeFile(p, b)
		})
		if err != nil {
			return Report{}, fmt.Errorf("copy assets: %w", err)
		}
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	steps := []func() error{
		func() error { return w.writeJSON("data/meta.json", buildMeta(idx, opts.FeedName, now)) },
		func() error { return w.writeJSON("data/overview.json", buildOverview(idx)) },
		func() error { return w.writeJSON("data/stops.json", buildStops(idx)) },
		func() error { return w.writeJSON("data/search.json", buildSearch(idx)) },
		func() error {
			// The journey planner's input, in a compact binary form.
			b, err := timetable.Encode(timetable.Build(idx))
			if err != nil {
				return fmt.Errorf("encode timetable: %w", err)
			}
			return w.writeFile("data/timetable.bin", b)
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return Report{}, err
		}
	}
	for _, rt := range idx.Routes {
		if err := w.writeJSON("data/routes/"+rt.ID+".json", buildRoute(rt)); err != nil {
			return Report{}, err
		}
	}
	for _, st := range idx.Stops {
		if err := w.writeJSON("data/stops/"+st.ID+".json", buildStop(st)); err != nil {
			return Report{}, err
		}
	}
	return w.rep, nil
}

type meta struct {
	Feed      string `json:"feed"`
	BuiltAt   string `json:"builtAt"`
	Routes    int    `json:"routes"`
	Stops     int    `json:"stops"`
	Trips     int    `json:"trips"`
	StopTimes int    `json:"stopTimes"`
}

func buildMeta(idx *index.Index, feed string, now time.Time) meta {
	return meta{
		Feed: feed, BuiltAt: now.UTC().Format(time.RFC3339),
		Routes: idx.Stats.Routes, Stops: idx.Stats.Stops, Trips: idx.Stats.Trips, StopTimes: idx.Stats.StopTimes,
	}
}

// buildOverview is every route's coarse geometry, for the faint network layer.
func buildOverview(idx *index.Index) featureCollection {
	fc := newCollection()
	for _, rt := range idx.Routes {
		for _, ln := range rt.Lines {
			if len(ln.Overview) < 2 {
				continue
			}
			fc.Features = append(fc.Features, feature{
				Type:       "Feature",
				Geometry:   newLine(ln.Overview),
				Properties: map[string]string{"id": rt.ID, "n": rt.Short},
			})
		}
	}
	return fc
}

// buildStops is every stop as a point, for clustering and the heat layer.
func buildStops(idx *index.Index) featureCollection {
	fc := newCollection()
	for _, st := range idx.Stops {
		fc.Features = append(fc.Features, feature{
			Type:     "Feature",
			Geometry: newPoint(st.Lon, st.Lat),
			Properties: map[string]any{
				"id": st.ID, "name": st.Name, "trips": st.Trips, "routes": len(st.Routes),
			},
		})
	}
	return fc
}

// buildSearch lists routes as compact [id, shortLabel, longName, trips] rows
// so the page can search them without a server.
func buildSearch(idx *index.Index) map[string]any {
	rows := make([][4]any, len(idx.Routes))
	for i, rt := range idx.Routes {
		rows[i] = [4]any{rt.ID, rt.Short, rt.Long, rt.Trips}
	}
	return map[string]any{"routes": rows}
}

type routeInfo struct {
	ID       string `json:"id"`
	Short    string `json:"short"`
	Long     string `json:"long"`
	Trips    int    `json:"trips"`
	FirstDep string `json:"firstDeparture"`
	LastDep  string `json:"lastDeparture"`
}

type routeDetail struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
	Route    routeInfo `json:"route"`
}

func buildRoute(rt *index.Route) routeDetail {
	out := routeDetail{
		Type:     "FeatureCollection",
		Features: []feature{},
		Route: routeInfo{
			ID: rt.ID, Short: rt.Short, Long: rt.Long, Trips: rt.Trips,
			FirstDep: index.FormatTime(rt.FirstDep), LastDep: index.FormatTime(rt.LastDep),
		},
	}
	for _, ln := range rt.Lines {
		if len(ln.Coords) >= 2 {
			out.Features = append(out.Features, feature{
				Type:     "Feature",
				Geometry: newLine(ln.Coords),
				Properties: map[string]any{
					"kind": "line", "direction": ln.Direction, "shape": ln.ShapeID, "trips": ln.Trips,
				},
			})
		}
		for i, ls := range ln.Stops {
			out.Features = append(out.Features, feature{
				Type:     "Feature",
				Geometry: newPoint(ls.Stop.Lon, ls.Stop.Lat),
				Properties: map[string]any{
					"kind": "stop", "id": ls.Stop.ID, "name": ls.Stop.Name, "seq": i + 1,
					"time": index.FormatTime(ls.Departure), "direction": ln.Direction,
				},
			})
		}
	}
	return out
}

type stopRouteJSON struct {
	ID    string `json:"id"`
	Short string `json:"short"`
	Long  string `json:"long"`
	Trips int    `json:"trips"`
	// Deps are departure times in minutes since midnight (can exceed 1440 for
	// trips past midnight), ascending.
	Deps []int `json:"deps"`
}

type stopDetail struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Lat    float64         `json:"lat"`
	Lon    float64         `json:"lon"`
	Trips  int             `json:"trips"`
	Routes []stopRouteJSON `json:"routes"`
}

func buildStop(st *index.Stop) stopDetail {
	out := stopDetail{ID: st.ID, Name: st.Name, Lat: st.Lat, Lon: st.Lon, Trips: st.Trips, Routes: make([]stopRouteJSON, len(st.Routes))}
	for i, sr := range st.Routes {
		deps := make([]int, len(sr.Departures))
		for j, d := range sr.Departures {
			deps[j] = d / 60
		}
		out.Routes[i] = stopRouteJSON{
			ID: sr.Route.ID, Short: sr.Route.Short, Long: sr.Route.Long, Trips: sr.Trips, Deps: deps,
		}
	}
	return out
}
