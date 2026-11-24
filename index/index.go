// Package index joins a GTFS feed into the compact structures the map needs.
package index

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jitinchekka2/tgbus/gtfs"
)

// Simplification tolerances in metres.
const (
	DetailTolerance   = 10.0
	OverviewTolerance = 50.0
)

// Index is the read-only, query-ready view of a feed.
type Index struct {
	Routes []*Route // sorted by short name
	Stops  []*Stop
	Stats  Stats

	routeByID map[string]*Route
	stopByID  map[string]*Stop
}

// Stats summarises what was loaded, for startup logging and sanity checks.
type Stats struct {
	Routes, Trips, Stops, StopTimes, Shapes int
	Skipped                                 map[string]int // rows dropped, by reason
}

// Route is a bus route with its drawable lines.
type Route struct {
	ID, Short, Long string
	Trips           int // trips per day (the feed has a single all-days service)
	FirstDep        int // earliest first-stop departure in seconds, -1 if unknown
	LastDep         int // latest first-stop departure in seconds, -1 if unknown
	Lines           []*Line

	searchShort, searchLong string
}

// Line is one stopping pattern of a route, drawn along one shape.
type Line struct {
	ShapeID   string
	Direction int
	Trips     int
	Coords    []Coord // detail geometry
	Overview  []Coord // coarser geometry for the all-routes layer
	Stops     []LineStop
}

// LineStop is a stop on a line, in travel order, with the representative
// trip's departure time.
type LineStop struct {
	Stop      *Stop
	Departure int // seconds, -1 if unknown
}

// Stop is a bus stop with service statistics.
type Stop struct {
	ID, Name string
	Lat, Lon float64
	Trips    int         // trips per day calling here
	Routes   []StopRoute // routes calling here, busiest first
}

// StopRoute is a route calling at a stop and how many trips per day.
type StopRoute struct {
	Route *Route
	Trips int
	// Departures are the seconds-since-midnight at which trips of this route
	// leave the stop, ascending. Values can exceed 86400 for trips past
	// midnight. Trips that end at the stop or have no usable time are omitted,
	// so len(Departures) can be smaller than Trips.
	Departures []int
}

type rawStopTime struct{ seq, stop, dep int32 }

// Load reads the feed in dir and builds an Index.
func Load(dir string) (*Index, error) {
	files := gtfs.FeedFiles(dir)
	skipped := map[string]int{}
	note := func(reason string, n int) {
		if n > 0 {
			skipped[reason] += n
		}
	}

	routes, n, err := gtfs.LoadRoutes(files.Routes)
	if err != nil {
		return nil, err
	}
	note("routes without id", n)
	trips, n, err := gtfs.LoadTrips(files.Trips)
	if err != nil {
		return nil, err
	}
	note("trips without id or route", n)
	stops, n, err := gtfs.LoadStops(files.Stops)
	if err != nil {
		return nil, err
	}
	note("stops with bad id or coordinates", n)
	shapes, n, err := gtfs.LoadShapes(files.Shapes)
	if err != nil {
		return nil, err
	}
	note("shape points with bad values", n)

	idx := &Index{
		routeByID: make(map[string]*Route, len(routes)),
		stopByID:  make(map[string]*Stop, len(stops)),
	}
	stopPos := make(map[string]int32, len(stops))
	for _, s := range stops {
		if _, dup := idx.stopByID[s.ID]; dup {
			note("duplicate stop ids", 1)
			continue
		}
		st := &Stop{ID: s.ID, Name: s.Name, Lat: s.Lat, Lon: s.Lon}
		stopPos[s.ID] = int32(len(idx.Stops))
		idx.Stops = append(idx.Stops, st)
		idx.stopByID[s.ID] = st
	}
	for _, r := range routes {
		if _, dup := idx.routeByID[r.ID]; dup {
			note("duplicate route ids", 1)
			continue
		}
		rt := &Route{
			ID: r.ID, Short: r.ShortName, Long: r.LongName,
			FirstDep: -1, LastDep: -1,
			searchShort: strings.ToLower(r.ShortName),
			searchLong:  strings.ToLower(r.LongName),
		}
		idx.Routes = append(idx.Routes, rt)
		idx.routeByID[r.ID] = rt
	}

	// Trips: remember each trip's route and index, grouped per route.
	tripPos := make(map[string]int32, len(trips))
	tripsOfRoute := make(map[*Route][]int32)
	for i, t := range trips {
		rt, ok := idx.routeByID[t.RouteID]
		if !ok {
			note("trips with unknown route", 1)
			continue
		}
		if _, dup := tripPos[t.ID]; dup {
			note("duplicate trip ids", 1)
			continue
		}
		tripPos[t.ID] = int32(i)
		tripsOfRoute[rt] = append(tripsOfRoute[rt], int32(i))
	}

	// Stop times: stream (the file is ~60 MB) into compact per-trip lists.
	perTrip := make([][]rawStopTime, len(trips))
	total := 0
	n, err = gtfs.ReadStopTimes(files.StopTimes, func(st gtfs.StopTime) {
		ti, ok := tripPos[st.TripID]
		if !ok {
			note("stop times with unknown trip", 1)
			return
		}
		si, ok := stopPos[st.StopID]
		if !ok {
			note("stop times with unknown stop", 1)
			return
		}
		perTrip[ti] = append(perTrip[ti], rawStopTime{int32(st.Seq), si, int32(st.Departure)})
		total++
	})
	if err != nil {
		return nil, err
	}
	note("stop times with bad values", n)
	for _, list := range perTrip {
		if !sort.SliceIsSorted(list, func(i, j int) bool { return list[i].seq < list[j].seq }) {
			sort.Slice(list, func(i, j int) bool { return list[i].seq < list[j].seq })
		}
	}

	// Per-route lines and per-stop statistics.
	stopRoutes := make([]map[*Route][]int, len(idx.Stops))
	geom := make(map[string]shapeGeom)
	for _, rt := range idx.Routes {
		idx.buildRoute(rt, tripsOfRoute[rt], trips, perTrip, shapes, geom, stopRoutes)
	}
	for i, m := range stopRoutes {
		st := idx.Stops[i]
		for rt, deps := range m {
			sort.Ints(deps)
			// Unknown times are stored as -1 so they still count as trips.
			unknown := sort.SearchInts(deps, 0)
			st.Routes = append(st.Routes, StopRoute{Route: rt, Trips: len(deps), Departures: deps[unknown:]})
			st.Trips += len(deps)
		}
		sort.Slice(st.Routes, func(a, b int) bool {
			ra, rb := st.Routes[a], st.Routes[b]
			if ra.Trips != rb.Trips {
				return ra.Trips > rb.Trips
			}
			return ra.Route.Short < rb.Route.Short
		})
	}

	sort.SliceStable(idx.Routes, func(i, j int) bool {
		a, b := idx.Routes[i], idx.Routes[j]
		if a.Short != b.Short {
			return naturalLess(a.Short, b.Short)
		}
		return a.ID < b.ID
	})

	idx.Stats = Stats{
		Routes: len(idx.Routes), Trips: len(trips), Stops: len(idx.Stops),
		StopTimes: total, Shapes: len(shapes), Skipped: skipped,
	}
	return idx, nil
}

type shapeGeom struct{ detail, overview []Coord }

type group struct {
	shapeID  string
	dir      int
	trips    int
	rep      int32 // trip with the most stops; its stop list defines the line
	repStops int
}

func (idx *Index) buildRoute(rt *Route, tripIDs []int32, trips []gtfs.Trip, perTrip [][]rawStopTime,
	shapes map[string][]gtfs.Point, geom map[string]shapeGeom, stopRoutes []map[*Route][]int) {

	rt.Trips = len(tripIDs)
	var order []string
	groups := map[string]*group{}
	for _, ti := range tripIDs {
		list := perTrip[ti]
		if len(list) == 0 {
			continue
		}
		if d := int(list[0].dep); d >= 0 {
			if rt.FirstDep < 0 || d < rt.FirstDep {
				rt.FirstDep = d
			}
			if d > rt.LastDep {
				rt.LastDep = d
			}
		}
		for i, st := range list {
			m := stopRoutes[st.stop]
			if m == nil {
				m = make(map[*Route][]int)
				stopRoutes[st.stop] = m
			}
			dep := int(st.dep)
			if i == len(list)-1 {
				dep = -1 // the last stop of a trip is an arrival, not a departure
			}
			m[rt] = append(m[rt], dep)
		}

		t := trips[ti]
		key := t.ShapeID
		if key == "" {
			key = fmt.Sprintf("dir:%d", t.Direction)
		}
		g, ok := groups[key]
		if !ok {
			g = &group{shapeID: t.ShapeID, dir: t.Direction, rep: ti, repStops: len(list)}
			groups[key] = g
			order = append(order, key)
		}
		g.trips++
		if len(list) > g.repStops {
			g.rep, g.repStops = ti, len(list)
		}
	}

	for _, key := range order {
		g := groups[key]
		line := &Line{ShapeID: g.shapeID, Direction: g.dir, Trips: g.trips}
		for _, st := range perTrip[g.rep] {
			line.Stops = append(line.Stops, LineStop{Stop: idx.Stops[st.stop], Departure: int(st.dep)})
		}

		if pts, ok := shapes[g.shapeID]; ok && g.shapeID != "" && len(pts) >= 2 {
			sg, cached := geom[g.shapeID]
			if !cached {
				cs := make([]Coord, len(pts))
				for i, p := range pts {
					cs[i] = Coord{p.Lon, p.Lat}
				}
				sg = shapeGeom{
					detail:   RoundCoords(Simplify(cs, DetailTolerance)),
					overview: RoundCoords(Simplify(cs, OverviewTolerance)),
				}
				geom[g.shapeID] = sg
			}
			line.Coords, line.Overview = sg.detail, sg.overview
		} else {
			// No usable shape: fall back to a straight line through the stops.
			cs := make([]Coord, 0, len(line.Stops))
			for _, ls := range line.Stops {
				cs = append(cs, Coord{ls.Stop.Lon, ls.Stop.Lat})
			}
			cs = RoundCoords(cs)
			line.Coords, line.Overview = cs, cs
		}
		rt.Lines = append(rt.Lines, line)
	}
}

// Route returns the route with the given id.
func (idx *Index) Route(id string) (*Route, bool) {
	r, ok := idx.routeByID[id]
	return r, ok
}

// Stop returns the stop with the given id.
func (idx *Index) Stop(id string) (*Stop, bool) {
	s, ok := idx.stopByID[id]
	return s, ok
}

// Search finds routes whose short or long name matches q (case-insensitive).
// Exact short-name matches come first, then short-name prefixes, short-name
// substrings and long-name substrings. At most limit routes are returned.
func (idx *Index) Search(q string, limit int) []*Route {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" || limit <= 0 {
		return nil
	}
	type hit struct {
		r     *Route
		score int
	}
	var hits []hit
	for _, r := range idx.Routes {
		switch {
		case r.searchShort == q:
			hits = append(hits, hit{r, 0})
		case strings.HasPrefix(r.searchShort, q):
			hits = append(hits, hit{r, 1})
		case strings.Contains(r.searchShort, q):
			hits = append(hits, hit{r, 2})
		case strings.Contains(r.searchLong, q):
			hits = append(hits, hit{r, 3})
		}
	}
	// idx.Routes is already in natural short-name order; keep it within a score.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]*Route, len(hits))
	for i, h := range hits {
		out[i] = h.r
	}
	return out
}

// FormatTime renders seconds since the start of the service day as HH:MM.
// Times past midnight keep counting (25:10), as in GTFS.
func FormatTime(sec int) string {
	if sec < 0 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", sec/3600, sec%3600/60)
}

// naturalLess orders "9A" before "10A" by comparing digit runs numerically.
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			si := i
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			sj := j
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na, nb := strings.TrimLeft(a[si:i], "0"), strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if a[i] != b[j] {
			return a[i] < b[j]
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
