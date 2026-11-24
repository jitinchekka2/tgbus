package gtfs

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Route is a row of routes.txt.
type Route struct {
	ID, ShortName, LongName string
}

// Trip is a row of trips.txt.
type Trip struct {
	ID, RouteID, ShapeID string
	Direction            int
}

// Stop is a row of stops.txt.
type Stop struct {
	ID, Name string
	Lat, Lon float64
}

// StopTime is a row of stop_times.txt. Departure is seconds since the start of
// the service day, or -1 when neither departure nor arrival time is usable.
type StopTime struct {
	TripID, StopID string
	Seq            int
	Departure      int
}

// Point is a shape vertex.
type Point struct{ Lat, Lon float64 }

func clone(s string) string { return strings.Clone(s) }

// LoadRoutes reads routes.txt. Rows without a route_id are skipped and counted.
func LoadRoutes(path string) (routes []Route, skipped int, err error) {
	err = readFile(path, []string{"route_id"}, func(r Row) error {
		id := r.Get("route_id")
		if id == "" {
			skipped++
			return nil
		}
		routes = append(routes, Route{
			ID:        clone(id),
			ShortName: clone(r.Get("route_short_name")),
			LongName:  clone(r.Get("route_long_name")),
		})
		return nil
	})
	return routes, skipped, err
}

// LoadTrips reads trips.txt. Rows without a trip_id or route_id are skipped and counted.
func LoadTrips(path string) (trips []Trip, skipped int, err error) {
	err = readFile(path, []string{"route_id", "trip_id"}, func(r Row) error {
		id, rid := r.Get("trip_id"), r.Get("route_id")
		if id == "" || rid == "" {
			skipped++
			return nil
		}
		dir, _ := strconv.Atoi(r.Get("direction_id"))
		trips = append(trips, Trip{
			ID:        clone(id),
			RouteID:   clone(rid),
			ShapeID:   clone(r.Get("shape_id")),
			Direction: dir,
		})
		return nil
	})
	return trips, skipped, err
}

// LoadStops reads stops.txt. Rows with a missing id or unparsable coordinates
// are skipped and counted.
func LoadStops(path string) (stops []Stop, skipped int, err error) {
	err = readFile(path, []string{"stop_id", "stop_lat", "stop_lon"}, func(r Row) error {
		id := r.Get("stop_id")
		lat, e1 := strconv.ParseFloat(r.Get("stop_lat"), 64)
		lon, e2 := strconv.ParseFloat(r.Get("stop_lon"), 64)
		if id == "" || e1 != nil || e2 != nil {
			skipped++
			return nil
		}
		stops = append(stops, Stop{ID: clone(id), Name: clone(r.Get("stop_name")), Lat: lat, Lon: lon})
		return nil
	})
	return stops, skipped, err
}

// LoadShapes reads shapes.txt into one ordered point list per shape_id. Rows
// need not be grouped or sorted in the file. Bad rows are skipped and counted.
func LoadShapes(path string) (shapes map[string][]Point, skipped int, err error) {
	type seqPoint struct {
		seq int
		Point
	}
	raw := make(map[string][]seqPoint)
	err = readFile(path, []string{"shape_id", "shape_pt_lat", "shape_pt_lon", "shape_pt_sequence"}, func(r Row) error {
		id := r.Get("shape_id")
		lat, e1 := strconv.ParseFloat(r.Get("shape_pt_lat"), 64)
		lon, e2 := strconv.ParseFloat(r.Get("shape_pt_lon"), 64)
		seq, e3 := strconv.Atoi(r.Get("shape_pt_sequence"))
		if id == "" || e1 != nil || e2 != nil || e3 != nil {
			skipped++
			return nil
		}
		pts, ok := raw[id]
		if !ok {
			id = clone(id)
		}
		raw[id] = append(pts, seqPoint{seq, Point{lat, lon}})
		return nil
	})
	if err != nil {
		return nil, skipped, err
	}
	shapes = make(map[string][]Point, len(raw))
	for id, pts := range raw {
		sort.SliceStable(pts, func(i, j int) bool { return pts[i].seq < pts[j].seq })
		out := make([]Point, len(pts))
		for i, p := range pts {
			out[i] = p.Point
		}
		shapes[id] = out
	}
	return shapes, skipped, nil
}

// ReadStopTimes streams stop_times.txt (which is large) without retaining
// rows. Rows with a missing trip/stop or unparsable stop_sequence are skipped
// and counted. Departure falls back to arrival_time, then -1.
func ReadStopTimes(path string, fn func(StopTime)) (skipped int, err error) {
	err = readFile(path, []string{"trip_id", "stop_id", "stop_sequence"}, func(r Row) error {
		trip, stop := r.Get("trip_id"), r.Get("stop_id")
		seq, e := strconv.Atoi(r.Get("stop_sequence"))
		if trip == "" || stop == "" || e != nil {
			skipped++
			return nil
		}
		dep := -1
		if t, e := ParseTime(r.Get("departure_time")); e == nil {
			dep = t
		} else if t, e := ParseTime(r.Get("arrival_time")); e == nil {
			dep = t
		}
		fn(StopTime{TripID: trip, StopID: stop, Seq: seq, Departure: dep})
		return nil
	})
	return skipped, err
}

// Files holds the paths of the feed files used by this program.
type Files struct{ Routes, Trips, Stops, StopTimes, Shapes string }

// FeedFiles returns the standard file locations inside a feed directory.
func FeedFiles(dir string) Files {
	return Files{
		Routes:    filepath.Join(dir, "routes.txt"),
		Trips:     filepath.Join(dir, "trips.txt"),
		Stops:     filepath.Join(dir, "stops.txt"),
		StopTimes: filepath.Join(dir, "stop_times.txt"),
		Shapes:    filepath.Join(dir, "shapes.txt"),
	}
}
