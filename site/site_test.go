package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jitinchekka2/tgbus/index"
)

func TestCheckIDs(t *testing.T) {
	tests := []struct {
		name    string
		ids     []string
		wantErr string
	}{
		{name: "plain ids", ids: []string{"RgqMpbyP", "abc_12", "x-y"}},
		{name: "empty list", ids: nil},
		{name: "path separator", ids: []string{"a/b"}, wantErr: "unsafe"},
		{name: "parent directory", ids: []string{".."}, wantErr: "unsafe"},
		{name: "space", ids: []string{"a b"}, wantErr: "unsafe"},
		{name: "empty id", ids: []string{""}, wantErr: "unsafe"},
		{name: "case-only collision", ids: []string{"AbC", "aBc"}, wantErr: "differ only by case"},
		{name: "exact duplicate", ids: []string{"abc", "abc"}, wantErr: "differ only by case"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkIDs("route", tt.ids)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func buildFixture(t *testing.T) (string, Report) {
	t.Helper()
	idx, err := index.Load("../testdata/feed")
	if err != nil {
		t.Fatalf("index.Load: %v", err)
	}
	out := filepath.Join(t.TempDir(), "dist")
	assets := fstest.MapFS{
		"index.html":   {Data: []byte("<h1>map</h1>")},
		"js/app.js":    {Data: []byte("//js")},
		"embed.go":     {Data: []byte("package web")},
		"vendor/x.css": {Data: []byte("a{}")},
	}
	rep, err := Build(idx, Options{
		Out: out, Assets: assets, FeedName: "fixture",
		Now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return out, rep
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestBuildFiles(t *testing.T) {
	out, rep := buildFixture(t)

	tests := []struct {
		name   string
		path   string
		exists bool
	}{
		{name: "index page copied", path: "index.html", exists: true},
		{name: "nested asset copied", path: "js/app.js", exists: true},
		{name: "vendor asset copied", path: "vendor/x.css", exists: true},
		{name: "go source not copied", path: "embed.go", exists: false},
		{name: "meta", path: "data/meta.json", exists: true},
		{name: "overview", path: "data/overview.json", exists: true},
		{name: "stops", path: "data/stops.json", exists: true},
		{name: "search", path: "data/search.json", exists: true},
		{name: "route file", path: "data/routes/R1.json", exists: true},
		{name: "route without trips still has a page", path: "data/routes/R3.json", exists: true},
		{name: "stop file", path: "data/stops/A.json", exists: true},
		{name: "skipped bad stop has no file", path: "data/stops/BAD.json", exists: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := os.Stat(filepath.Join(out, filepath.FromSlash(tt.path)))
			if (err == nil) != tt.exists {
				t.Errorf("exists = %v, want %v (err %v)", err == nil, tt.exists, err)
			}
		})
	}
	// 3 assets + meta/overview/stops/search + 3 routes + 4 stops.
	if rep.Files != 3+4+3+4 {
		t.Errorf("report files = %d, want %d", rep.Files, 3+4+3+4)
	}
}

func TestBuildMeta(t *testing.T) {
	out, _ := buildFixture(t)
	var got meta
	readJSON(t, filepath.Join(out, "data/meta.json"), &got)
	want := meta{Feed: "fixture", BuiltAt: "2026-01-02T03:04:05Z", Routes: 3, Stops: 4, Trips: 5, StopTimes: 10}
	if got != want {
		t.Errorf("meta = %+v, want %+v", got, want)
	}
}

func TestBuildSearchAndStops(t *testing.T) {
	out, _ := buildFixture(t)

	var search struct {
		Routes [][4]any `json:"routes"`
	}
	readJSON(t, filepath.Join(out, "data/search.json"), &search)
	if len(search.Routes) != 3 {
		t.Fatalf("search routes = %d, want 3", len(search.Routes))
	}
	if first := search.Routes[0]; first[0] != "R1" || first[1] != "10A" || first[2] != "10A - Koti, to Uppal" || first[3] != float64(3) {
		t.Errorf("first search row = %v", first)
	}

	var stops featureCollectionProps
	readJSON(t, filepath.Join(out, "data/stops.json"), &stops)
	if len(stops.Features) != 4 {
		t.Errorf("stops = %d, want 4", len(stops.Features))
	}

	var overview featureCollectionProps
	readJSON(t, filepath.Join(out, "data/overview.json"), &overview)
	if len(overview.Features) != 3 { // R1 has two lines, R2 one, R3 none
		t.Errorf("overview features = %d, want 3", len(overview.Features))
	}
}

type featureCollectionProps struct {
	Features []struct {
		Properties map[string]any `json:"properties"`
	} `json:"features"`
}

func TestBuildRouteFile(t *testing.T) {
	out, _ := buildFixture(t)
	tests := []struct {
		name                string
		id                  string
		wantLines, wantStop int
		wantFirst, wantLast string
	}{
		{name: "two lines", id: "R1", wantLines: 2, wantStop: 6, wantFirst: "08:00", wantLast: "25:10"},
		{name: "missing shape falls back to stops", id: "R2", wantLines: 1, wantStop: 2, wantFirst: "10:00", wantLast: "10:00"},
		{name: "no trips", id: "R3", wantFirst: "", wantLast: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Features []struct {
					Properties map[string]any `json:"properties"`
				} `json:"features"`
				Route routeInfo `json:"route"`
			}
			readJSON(t, filepath.Join(out, "data/routes", tt.id+".json"), &got)
			lines, stops := 0, 0
			for _, f := range got.Features {
				switch f.Properties["kind"] {
				case "line":
					lines++
				case "stop":
					stops++
				}
			}
			if lines != tt.wantLines || stops != tt.wantStop {
				t.Errorf("lines/stops = %d/%d, want %d/%d", lines, stops, tt.wantLines, tt.wantStop)
			}
			if got.Route.FirstDep != tt.wantFirst || got.Route.LastDep != tt.wantLast {
				t.Errorf("first/last = %q/%q, want %q/%q", got.Route.FirstDep, got.Route.LastDep, tt.wantFirst, tt.wantLast)
			}
		})
	}
}

func TestBuildStopFile(t *testing.T) {
	out, _ := buildFixture(t)
	tests := []struct {
		name  string
		id    string
		trips int
		deps  map[string][]int // route id -> departure minutes
	}{
		{
			name: "two routes, past-midnight trip, terminal arrival excluded", id: "A", trips: 4,
			deps: map[string][]int{"R1": {8 * 60, 25*60 + 10}, "R2": {10 * 60}},
		},
		{name: "end of line departs only its own trip", id: "C", trips: 3, deps: map[string][]int{"R1": {9 * 60}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got stopDetail
			readJSON(t, filepath.Join(out, "data/stops", tt.id+".json"), &got)
			if got.Trips != tt.trips {
				t.Errorf("trips = %d, want %d", got.Trips, tt.trips)
			}
			deps := map[string][]int{}
			for _, r := range got.Routes {
				deps[r.ID] = r.Deps
			}
			if !reflect.DeepEqual(deps, tt.deps) {
				t.Errorf("deps = %v, want %v", deps, tt.deps)
			}
		})
	}
}

func TestBuildRemovesStaleData(t *testing.T) {
	idx, err := index.Load("../testdata/feed")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	stale := filepath.Join(out, "data", "routes", "OLD.json")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(idx, Options{Out: out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file still present (err %v)", err)
	}
}

func TestBuildErrors(t *testing.T) {
	idx, err := index.Load("../testdata/feed")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		mutate  func(*index.Index)
		out     string
		wantErr string
	}{
		{name: "no output dir", out: "", wantErr: "output directory is required"},
		{
			name: "unsafe route id", out: t.TempDir(), wantErr: "unsafe",
			mutate: func(i *index.Index) { i.Routes[0].ID = "../evil" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Work on a shallow copy so one case cannot affect another.
			cp := *idx
			cp.Routes = append([]*index.Route(nil), idx.Routes...)
			if tt.mutate != nil {
				r := *cp.Routes[0]
				cp.Routes[0] = &r
				tt.mutate(&cp)
			}
			_, err := Build(&cp, Options{Out: tt.out})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
