package index

import (
	"reflect"
	"sort"
	"testing"

	"github.com/jitinchekka2/tgbus/gtfs"
)

const fixture = "../testdata/feed"

func loadFixture(t *testing.T) *Index {
	t.Helper()
	idx, err := Load(fixture)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return idx
}

func TestSimplify(t *testing.T) {
	tests := []struct {
		name string
		in   []Coord
		tol  float64
		want []Coord
	}{
		{name: "empty", in: nil, tol: 10, want: nil},
		{name: "one point", in: []Coord{{78, 17}}, tol: 10, want: []Coord{{78, 17}}},
		{name: "two points", in: []Coord{{78, 17}, {78.1, 17.1}}, tol: 10, want: []Coord{{78, 17}, {78.1, 17.1}}},
		{
			name: "collinear middle dropped",
			in:   []Coord{{78, 17}, {78.005, 17}, {78.01, 17}},
			tol:  10,
			want: []Coord{{78, 17}, {78.01, 17}},
		},
		{
			// 0.001 deg lat is ~110 m off the line: far above a 10 m tolerance.
			name: "spike kept",
			in:   []Coord{{78, 17}, {78.005, 17.001}, {78.01, 17}},
			tol:  10,
			want: []Coord{{78, 17}, {78.005, 17.001}, {78.01, 17}},
		},
		{
			name: "spike dropped when tolerance is larger",
			in:   []Coord{{78, 17}, {78.005, 17.001}, {78.01, 17}},
			tol:  500,
			want: []Coord{{78, 17}, {78.01, 17}},
		},
		{
			name: "zero tolerance keeps everything",
			in:   []Coord{{78, 17}, {78.005, 17}, {78.01, 17}},
			tol:  0,
			want: []Coord{{78, 17}, {78.005, 17}, {78.01, 17}},
		},
		{
			// First and last point are equal: the far vertex must survive.
			name: "closed loop",
			in:   []Coord{{78, 17}, {78.01, 17}, {78.01, 17.01}, {78, 17}},
			tol:  10,
			want: []Coord{{78, 17}, {78.01, 17}, {78.01, 17.01}, {78, 17}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := append([]Coord(nil), tt.in...)
			got := Simplify(tt.in, tt.tol)
			if !reflect.DeepEqual(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("Simplify = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.in, orig) && len(orig) > 0 {
				t.Errorf("input was modified: %v -> %v", orig, tt.in)
			}
		})
	}
}

func TestRoundCoords(t *testing.T) {
	tests := []struct {
		name string
		in   []Coord
		want []Coord
	}{
		{name: "rounds to 5 places", in: []Coord{{78.1234567, 17.9876543}}, want: []Coord{{78.12346, 17.98765}}},
		{name: "drops consecutive duplicates", in: []Coord{{78.000001, 17}, {78.000002, 17}, {78.1, 17}}, want: []Coord{{78, 17}, {78.1, 17}}},
		{name: "keeps non-consecutive repeats", in: []Coord{{1, 1}, {2, 2}, {1, 1}}, want: []Coord{{1, 1}, {2, 2}, {1, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RoundCoords(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RoundCoords = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNaturalLess(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "9 before 10", a: "9A", b: "10A", want: true},
		{name: "10 not before 9", a: "10A", b: "9A", want: false},
		{name: "same prefix, suffix decides", a: "10A", b: "10B", want: true},
		{name: "equal", a: "10A", b: "10A", want: false},
		{name: "prefix shorter first", a: "10", b: "10A", want: true},
		{name: "leading zeros ignored", a: "007", b: "8", want: true},
		{name: "letters before digits in ascii", a: "A1", b: "B1", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := naturalLess(tt.a, tt.b); got != tt.want {
				t.Errorf("naturalLess(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want string
	}{
		{name: "unknown", in: -1, want: ""},
		{name: "midnight", in: 0, want: "00:00"},
		{name: "morning", in: 8*3600 + 5*60 + 59, want: "08:05"},
		{name: "past midnight", in: 25*3600 + 10*60, want: "25:10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTime(tt.in); got != tt.want {
				t.Errorf("FormatTime(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLoadFixtureStats(t *testing.T) {
	idx := loadFixture(t)
	if got, want := idx.Stats.Routes, 3; got != want {
		t.Errorf("routes = %d, want %d", got, want)
	}
	if got, want := idx.Stats.Stops, 4; got != want {
		t.Errorf("stops = %d, want %d (bad-coordinate stop must be skipped)", got, want)
	}
	if got, want := idx.Stats.StopTimes, 10; got != want {
		t.Errorf("stop times = %d, want %d (unknown trip/stop rows must be skipped)", got, want)
	}
	wantSkipped := map[string]int{
		"stops with bad id or coordinates": 1,
		"trips with unknown route":         1,
		"stop times with unknown trip":     1,
		"stop times with unknown stop":     1,
	}
	if !reflect.DeepEqual(idx.Stats.Skipped, wantSkipped) {
		t.Errorf("skipped = %v, want %v", idx.Stats.Skipped, wantSkipped)
	}
}

func TestRouteSummary(t *testing.T) {
	idx := loadFixture(t)
	tests := []struct {
		name      string
		id        string
		trips     int
		first     int
		last      int
		lines     int
		lineStops []int // stops per line, in order
	}{
		{name: "two shapes, past-midnight trip", id: "R1", trips: 3, first: 8 * 3600, last: 25*3600 + 10*60, lines: 2, lineStops: []int{3, 3}},
		{name: "missing shape", id: "R2", trips: 1, first: 10 * 3600, last: 10 * 3600, lines: 1, lineStops: []int{2}},
		{name: "route without trips", id: "R3", trips: 0, first: -1, last: -1, lines: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := idx.Route(tt.id)
			if !ok {
				t.Fatalf("route %s not found", tt.id)
			}
			if r.Trips != tt.trips || r.FirstDep != tt.first || r.LastDep != tt.last || len(r.Lines) != tt.lines {
				t.Errorf("trips/first/last/lines = %d/%d/%d/%d, want %d/%d/%d/%d",
					r.Trips, r.FirstDep, r.LastDep, len(r.Lines), tt.trips, tt.first, tt.last, tt.lines)
			}
			for i, want := range tt.lineStops {
				if got := len(r.Lines[i].Stops); got != want {
					t.Errorf("line %d stops = %d, want %d", i, got, want)
				}
			}
		})
	}
}

func TestLineGeometry(t *testing.T) {
	idx := loadFixture(t)
	r1, _ := idx.Route("R1")

	// SH1 has a vertex on the straight A-B stretch that is dropped at 10 m,
	// and its out-of-order stop sequence (T1 rows 2,1,3) must be re-sorted.
	line := r1.Lines[0]
	if line.ShapeID != "SH1" || line.Direction != 0 || line.Trips != 2 {
		t.Fatalf("line = shape %q dir %d trips %d, want SH1/0/2", line.ShapeID, line.Direction, line.Trips)
	}
	var ids []string
	for _, ls := range line.Stops {
		ids = append(ids, ls.Stop.ID)
	}
	if want := []string{"A", "B", "C"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("stop order = %v, want %v", ids, want)
	}
	if got, want := line.Coords[0], (Coord{78.486, 17.385}); got != want {
		t.Errorf("first coord = %v, want %v", got, want)
	}
	if last := line.Coords[len(line.Coords)-1]; last != (Coord{78.56, 17.4}) {
		t.Errorf("last coord = %v, want {78.56 17.4}", last)
	}
	if len(line.Coords) >= 5 {
		t.Errorf("expected simplification to drop vertices, got %d coords", len(line.Coords))
	}
	if line.Stops[0].Departure != 8*3600 {
		t.Errorf("first departure = %d, want %d", line.Stops[0].Departure, 8*3600)
	}

	// R2's shape SH3 does not exist, so the line falls back to the stops.
	r2, _ := idx.Route("R2")
	want := []Coord{{78.486, 17.385}, {78.526, 17.368}}
	if got := r2.Lines[0].Coords; !reflect.DeepEqual(got, want) {
		t.Errorf("fallback coords = %v, want %v", got, want)
	}
}

func TestStopStats(t *testing.T) {
	idx := loadFixture(t)
	tests := []struct {
		name   string
		id     string
		trips  int
		routes map[string]int
		deps   map[string][]int // departures per route (arrivals at the last stop excluded)
	}{
		{
			name: "shared by two routes", id: "A", trips: 4,
			routes: map[string]int{"R1": 3, "R2": 1},
			// R1: T1 08:00 and T2 25:10 depart; T3 ends here at 09:30 so is not a departure.
			deps: map[string][]int{"R1": {8 * 3600, 25*3600 + 10*60}, "R2": {10 * 3600}},
		},
		{
			name: "single route", id: "B", trips: 2, routes: map[string]int{"R1": 2},
			deps: map[string][]int{"R1": {8*3600 + 10*60, 9*3600 + 20*60}},
		},
		{
			name: "end of line", id: "C", trips: 3, routes: map[string]int{"R1": 3},
			// T1 and T2 end at C; only T3 departs from it.
			deps: map[string][]int{"R1": {9 * 3600}},
		},
		{
			// T4's final stop ZZ is not in stops.txt and is dropped, so D is the last
			// known stop of that trip and counts as its end: no departures.
			name: "served by R2 only", id: "D", trips: 1, routes: map[string]int{"R2": 1},
			deps: map[string][]int{"R2": {}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ok := idx.Stop(tt.id)
			if !ok {
				t.Fatalf("stop %s not found", tt.id)
			}
			if s.Trips != tt.trips {
				t.Errorf("trips = %d, want %d", s.Trips, tt.trips)
			}
			got := map[string]int{}
			for _, sr := range s.Routes {
				got[sr.Route.ID] = sr.Trips
			}
			if !reflect.DeepEqual(got, tt.routes) {
				t.Errorf("routes = %v, want %v", got, tt.routes)
			}
			gotDeps := map[string][]int{}
			for _, sr := range s.Routes {
				gotDeps[sr.Route.ID] = sr.Departures
			}
			if !reflect.DeepEqual(gotDeps, tt.deps) {
				t.Errorf("departures = %v, want %v", gotDeps, tt.deps)
			}
			if !sort.SliceIsSorted(s.Routes, func(i, j int) bool { return s.Routes[i].Trips > s.Routes[j].Trips }) {
				t.Errorf("routes not sorted busiest first: %v", s.Routes)
			}
		})
	}
	if _, ok := idx.Stop("BAD"); ok {
		t.Error("stop with unparsable coordinates must not be indexed")
	}
}

func TestSearch(t *testing.T) {
	idx := loadFixture(t)
	tests := []struct {
		name  string
		q     string
		limit int
		want  []string // route ids
	}{
		{name: "exact short name", q: "10a", limit: 10, want: []string{"R1"}},
		{name: "short name prefix", q: "2", limit: 10, want: []string{"R2"}},
		{name: "long name substring, case-insensitive", q: "ALPHA", limit: 10, want: []string{"R2"}},
		{name: "long name with comma", q: "koti,", limit: 10, want: []string{"R1"}},
		{name: "surrounding space trimmed", q: "  30 ", limit: 10, want: []string{"R3"}},
		{name: "no match", q: "zzz", limit: 10, want: nil},
		{name: "empty query", q: "", limit: 10, want: nil},
		{name: "limit applied", q: "t", limit: 1, want: []string{"R1"}},
		{name: "zero limit", q: "10", limit: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, r := range idx.Search(tt.q, tt.limit) {
				got = append(got, r.ID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Search(%q, %d) = %v, want %v", tt.q, tt.limit, got, tt.want)
			}
		})
	}
}

func TestTripCalls(t *testing.T) {
	idx := loadFixture(t)
	type call struct {
		stop string
		dep  int32
	}
	tests := []struct {
		name  string
		route string
		calls []call
	}{
		{name: "rows given out of order are sorted", route: "R1", calls: []call{{"A", 8 * 3600}, {"B", 8*3600 + 600}, {"C", 8*3600 + 1800}}},
		{name: "past-midnight trip keeps times above 24h", route: "R1", calls: []call{{"A", 25*3600 + 600}, {"C", 25*3600 + 2400}}},
		{name: "opposite direction", route: "R1", calls: []call{{"C", 9 * 3600}, {"B", 9*3600 + 1200}, {"A", 9*3600 + 1800}}},
		{name: "stop missing from stops.txt is dropped", route: "R2", calls: []call{{"A", 10 * 3600}, {"D", 10*3600 + 600}}},
	}
	// Trips with an unknown route (T5) are excluded, so exactly the four above remain, in feed order.
	if len(idx.Trips) != len(tests) {
		t.Fatalf("trips = %d, want %d", len(idx.Trips), len(tests))
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := idx.Trips[i]
			if tc.Route.ID != tt.route {
				t.Errorf("route = %s, want %s", tc.Route.ID, tt.route)
			}
			var got []call
			for j := range tc.Stop {
				got = append(got, call{idx.Stops[tc.Stop[j]].ID, tc.Dep[j]})
			}
			if !reflect.DeepEqual(got, tt.calls) {
				t.Errorf("calls = %v, want %v", got, tt.calls)
			}
		})
	}
}

func TestBuildTripCallsRepairs(t *testing.T) {
	rt := &Route{ID: "R"}
	idx := &Index{routeByID: map[string]*Route{"R": rt}}
	tests := []struct {
		name     string
		in       []rawStopTime
		wantDeps []int32 // nil means the trip is dropped
		notes    map[string]int
	}{
		{name: "clean trip", in: []rawStopTime{{1, 0, 100}, {2, 1, 200}}, wantDeps: []int32{100, 200}},
		{
			name: "missing time filled from previous", in: []rawStopTime{{1, 0, 100}, {2, 1, -1}, {3, 2, 300}},
			wantDeps: []int32{100, 100, 300},
			notes:    map[string]int{"stop times with a missing time (filled from the previous stop)": 1},
		},
		{
			name: "backwards time raised", in: []rawStopTime{{1, 0, 100}, {2, 1, 90}},
			wantDeps: []int32{100, 100},
			notes:    map[string]int{"stop times running backwards (raised to the previous stop)": 1},
		},
		{
			name: "no time at the first stop drops the trip", in: []rawStopTime{{1, 0, -1}, {2, 1, 90}},
			notes: map[string]int{"trips dropped: no departure time at the first stop": 1},
		},
		{name: "single call dropped", in: []rawStopTime{{1, 0, 100}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notes := map[string]int{}
			note := func(r string, n int) { notes[r] += n }
			got := buildTripCalls(idx, []gtfs.Trip{{RouteID: "R"}}, [][]rawStopTime{tt.in}, note)
			if tt.wantDeps == nil {
				if len(got) != 0 {
					t.Fatalf("trip kept, want dropped: %+v", got)
				}
			} else if len(got) != 1 || !reflect.DeepEqual(got[0].Dep, tt.wantDeps) {
				t.Fatalf("got %+v, want deps %v", got, tt.wantDeps)
			}
			want := tt.notes
			if want == nil {
				want = map[string]int{}
			}
			if !reflect.DeepEqual(notes, want) {
				t.Errorf("notes = %v, want %v", notes, want)
			}
		})
	}
}
