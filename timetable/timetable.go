// Package timetable is the compact, query-ready form of a bus network that
// the journey planner runs on. It is built from an index.Index and has a
// small binary encoding so a browser can download it.
//
// Trips that call at the same stops in the same order on the same route form
// a Pattern. Most trips of a pattern share the same run times between stops,
// so a pattern stores a few time Profiles and each Trip only a first
// departure plus the profile it follows.
package timetable

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/jitinchekka2/tgbus/index"
)

// Timetable is a whole network. Stop, route and pattern order is
// deterministic for a given feed.
type Timetable struct {
	Stops    []Stop
	Routes   []Route
	Patterns []Pattern
}

// Stop is a place where buses call. Coordinates are rounded to 1e-6 degrees
// (about 10 cm) so they survive the binary encoding exactly.
type Stop struct {
	ID, Name string
	Lat, Lon float64
}

// Route identifies a bus route for display.
type Route struct {
	ID, Short, Long string
}

// Pattern is the set of trips of one route that call at the same stops in
// the same order.
type Pattern struct {
	Route int32   // index into Timetable.Routes
	Stops []int32 // indexes into Timetable.Stops, at least two
	// Profiles are alternative run times: Profiles[p][i] is the number of
	// seconds after the first departure that the bus leaves Stops[i].
	// Profiles[p][0] is 0 and values never decrease.
	Profiles [][]int32
	// Trips are sorted by first departure.
	Trips []Trip
}

// Trip is one run of a pattern.
type Trip struct {
	Dep0    int32 // first departure, seconds since the start of the service day
	Profile int32 // index into Pattern.Profiles
}

// Counts returns the number of patterns, trips and stop calls.
func (t *Timetable) Counts() (patterns, trips, calls int) {
	for _, p := range t.Patterns {
		patterns++
		trips += len(p.Trips)
		calls += len(p.Trips) * len(p.Stops)
	}
	return
}

// quantise rounds a coordinate to the 1e-6 degree grid used by the encoding.
func quantise(v float64) float64 { return float64(int32(math.Round(v*1e6))) / 1e6 }

type patternKey string

func keyFor(route int32, stops []int32) patternKey {
	b := binary.AppendUvarint(nil, uint64(route))
	for _, s := range stops {
		b = binary.AppendUvarint(b, uint64(s))
	}
	return patternKey(b)
}

// Build groups the index's trips into patterns.
func Build(idx *index.Index) *Timetable {
	tt := &Timetable{Stops: make([]Stop, len(idx.Stops))}
	for i, s := range idx.Stops {
		tt.Stops[i] = Stop{ID: s.ID, Name: s.Name, Lat: quantise(s.Lat), Lon: quantise(s.Lon)}
	}

	// Only routes that have trips are kept, in the index's (sorted) order.
	used := make(map[*index.Route]bool)
	for _, tc := range idx.Trips {
		used[tc.Route] = true
	}
	routeIdx := make(map[*index.Route]int32)
	for _, r := range idx.Routes {
		if used[r] {
			routeIdx[r] = int32(len(tt.Routes))
			tt.Routes = append(tt.Routes, Route{ID: r.ID, Short: r.Short, Long: r.Long})
		}
	}

	type pending struct {
		dep0    int32
		offsets []int32
	}
	byKey := make(map[patternKey]int)
	var pats []*Pattern
	var raw [][]pending
	for _, tc := range idx.Trips {
		r := routeIdx[tc.Route]
		k := keyFor(r, tc.Stop)
		pi, ok := byKey[k]
		if !ok {
			pi = len(pats)
			byKey[k] = pi
			pats = append(pats, &Pattern{Route: r, Stops: append([]int32(nil), tc.Stop...)})
			raw = append(raw, nil)
		}
		off := make([]int32, len(tc.Dep))
		for i, d := range tc.Dep {
			off[i] = d - tc.Dep[0]
		}
		raw[pi] = append(raw[pi], pending{dep0: tc.Dep[0], offsets: off})
	}

	for pi, p := range pats {
		trips := raw[pi]
		sort.SliceStable(trips, func(i, j int) bool { return trips[i].dep0 < trips[j].dep0 })
		profileOf := make(map[string]int32)
		for _, tr := range trips {
			sig := make([]byte, 0, len(tr.offsets)*2)
			for _, o := range tr.offsets {
				sig = binary.AppendUvarint(sig, uint64(o))
			}
			pr, ok := profileOf[string(sig)]
			if !ok {
				pr = int32(len(p.Profiles))
				profileOf[string(sig)] = pr
				p.Profiles = append(p.Profiles, tr.offsets)
			}
			p.Trips = append(p.Trips, Trip{Dep0: tr.dep0, Profile: pr})
		}
		tt.Patterns = append(tt.Patterns, *p)
	}
	return tt
}

// Validate checks the structural invariants the planner relies on.
func (t *Timetable) Validate() error {
	for i, p := range t.Patterns {
		if int(p.Route) < 0 || int(p.Route) >= len(t.Routes) {
			return fmt.Errorf("pattern %d: route %d out of range", i, p.Route)
		}
		if len(p.Stops) < 2 {
			return fmt.Errorf("pattern %d: needs at least two stops", i)
		}
		for _, s := range p.Stops {
			if int(s) < 0 || int(s) >= len(t.Stops) {
				return fmt.Errorf("pattern %d: stop %d out of range", i, s)
			}
		}
		if len(p.Trips) == 0 || len(p.Profiles) == 0 {
			return fmt.Errorf("pattern %d: no trips or profiles", i)
		}
		for j, pr := range p.Profiles {
			if len(pr) != len(p.Stops) || pr[0] != 0 {
				return fmt.Errorf("pattern %d profile %d: bad shape", i, j)
			}
			for k := 1; k < len(pr); k++ {
				if pr[k] < pr[k-1] {
					return fmt.Errorf("pattern %d profile %d: times decrease", i, j)
				}
			}
		}
		for j, tr := range p.Trips {
			if int(tr.Profile) < 0 || int(tr.Profile) >= len(p.Profiles) {
				return fmt.Errorf("pattern %d trip %d: profile %d out of range", i, j, tr.Profile)
			}
			if j > 0 && tr.Dep0 < p.Trips[j-1].Dep0 {
				return fmt.Errorf("pattern %d: trips not sorted by departure", i)
			}
		}
	}
	return nil
}
