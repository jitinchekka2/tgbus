package raptor

import (
	"fmt"
	"sort"

	"github.com/jitinchekka2/tgbus/timetable"
)

// stopAt makes a stop at a position in a local grid: x and y are in units of
// roughly 100 m, so neighbours at distance 1-4 are within the default
// 400 m transfer range and 5+ are not.
func stopAt(id string, x, y float64) timetable.Stop {
	return timetable.Stop{ID: id, Name: id, Lat: 17 + y*0.0009, Lon: 78 + x*0.00094}
}

// trip describes one run: first departure and seconds from the first
// departure to each stop (offsets[0] must be 0).
type trip struct {
	dep0    int32
	offsets []int32
}

type pat struct {
	route int32
	stops []int32
	trips []trip
}

// build assembles a Timetable from compact specs, one profile per trip.
func build(stops []timetable.Stop, nRoutes int, pats []pat) *timetable.Timetable {
	tt := &timetable.Timetable{Stops: stops}
	for i := 0; i < nRoutes; i++ {
		tt.Routes = append(tt.Routes, timetable.Route{ID: fmt.Sprintf("R%d", i), Short: fmt.Sprintf("%d", i)})
	}
	for _, p := range pats {
		trips := append([]trip(nil), p.trips...)
		sort.SliceStable(trips, func(i, j int) bool { return trips[i].dep0 < trips[j].dep0 })
		out := timetable.Pattern{Route: p.route, Stops: p.stops}
		for i, t := range trips {
			out.Profiles = append(out.Profiles, t.offsets)
			out.Trips = append(out.Trips, timetable.Trip{Dep0: t.dep0, Profile: int32(i)})
		}
		tt.Patterns = append(tt.Patterns, out)
	}
	return tt
}

// offsets builds a profile from per-hop travel times in seconds.
func offsets(hops ...int32) []int32 {
	out := []int32{0}
	for _, h := range hops {
		out = append(out, out[len(out)-1]+h)
	}
	return out
}

func hm(h, m int) int32 { return int32(h*3600 + m*60) }

// --- independent oracle: Connection Scan Algorithm -------------------------

type conn struct {
	from, to int32
	dep, arr int32
	trip     int
}

// connections expands every trip into its hops, for yesterday (only trips
// that run past midnight), today and tomorrow, written separately from the
// planner so a shared mistake is unlikely.
func connections(tt *timetable.Timetable) []conn {
	var out []conn
	id := 0
	for _, p := range tt.Patterns {
		for _, tr := range p.Trips {
			prof := p.Profiles[tr.Profile]
			last := tr.Dep0 + prof[len(prof)-1]
			for _, shift := range []int32{-86400, 0, 86400} {
				if shift < 0 && last < 86400 {
					continue
				}
				for i := 0; i+1 < len(p.Stops); i++ {
					out = append(out, conn{
						from: p.Stops[i], to: p.Stops[i+1],
						dep: tr.Dep0 + prof[i] + shift, arr: tr.Dep0 + prof[i+1] + shift, trip: id,
					})
				}
				id++
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].dep < out[j].dep })
	return out
}

// csa returns the earliest arrival at a destination, or inf if unreachable.
// It uses the same walking and transfer-buffer rules as the planner but none
// of its machinery (no rounds, no patterns, no classes).
func csa(n *Network, conns []conn, origins, dests []Access, depart, horizon int32) int32 {
	nS := len(n.TT.Stops)
	slack := n.opts.MinTransferSec
	ready := make([]int32, nS)   // earliest time a rider can board at the stop
	arrBest := make([]int32, nS) // earliest arrival by bus or by a walk after a bus
	for i := range ready {
		ready[i], arrBest[i] = inf, inf
	}
	for _, o := range origins {
		if t := depart + o.Sec; t < ready[o.Stop] {
			ready[o.Stop] = t
		}
	}
	maxTrip := 0
	for _, c := range conns {
		if c.trip > maxTrip {
			maxTrip = c.trip
		}
	}
	onTrip := make([]bool, maxTrip+1)
	for _, c := range conns {
		if c.dep < depart {
			continue
		}
		if c.dep > horizon {
			break
		}
		if !onTrip[c.trip] && (ready[c.from] == inf || c.dep < ready[c.from]) {
			continue
		}
		onTrip[c.trip] = true
		if c.arr < arrBest[c.to] {
			arrBest[c.to] = c.arr
		}
		if t := c.arr + slack; t < ready[c.to] {
			ready[c.to] = t
		}
		for _, f := range n.transfers[c.to] {
			if t := c.arr + f.sec; t < arrBest[f.to] {
				arrBest[f.to] = t
			}
			if t := c.arr + f.sec + slack; t < ready[f.to] {
				ready[f.to] = t
			}
		}
	}
	best := inf
	for _, d := range dests {
		if arrBest[d.Stop] != inf {
			if t := arrBest[d.Stop] + d.Sec; t < best {
				best = t
			}
		}
	}
	return best
}

// --- journey checks --------------------------------------------------------

// tripExists reports whether some trip (on any of the three days) of the
// route runs through the given stops contiguously with exactly these times.
func tripExists(tt *timetable.Timetable, l Leg) bool {
	for _, p := range tt.Patterns {
		if p.Route != l.Route {
			continue
		}
		for start := 0; start+len(l.Stops) <= len(p.Stops); start++ {
			match := true
			for j, s := range l.Stops {
				if p.Stops[start+j] != s {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			for _, tr := range p.Trips {
				prof := p.Profiles[tr.Profile]
				for _, shift := range []int32{-86400, 0, 86400} {
					d := tr.Dep0 + prof[start] + shift
					a := tr.Dep0 + prof[start+len(l.Stops)-1] + shift
					if d == l.Dep && a == l.Arr {
						return true
					}
				}
			}
		}
	}
	return false
}

// checkJourney returns a description of the first rule the journey breaks.
func checkJourney(n *Network, j Journey, depart int32) error {
	if len(j.Legs) == 0 {
		return fmt.Errorf("no legs")
	}
	first, last := j.Legs[0], j.Legs[len(j.Legs)-1]
	if first.From != -1 || last.To != -1 {
		return fmt.Errorf("journey must start at the origin and end at the destination point")
	}
	if first.Dep != depart {
		return fmt.Errorf("first leg departs %d, want the query time %d", first.Dep, depart)
	}
	if last.Arr != j.Arrive {
		return fmt.Errorf("last leg arrives %d, journey says %d", last.Arr, j.Arrive)
	}
	rides := 0
	for i, l := range j.Legs {
		if l.Arr < l.Dep {
			return fmt.Errorf("leg %d arrives before it departs", i)
		}
		if i > 0 {
			prev := j.Legs[i-1]
			if prev.To != l.From {
				return fmt.Errorf("leg %d starts at %d but leg %d ended at %d", i, l.From, i-1, prev.To)
			}
			need := prev.Arr
			if l.Mode == Ride && i > 1 {
				need += n.opts.MinTransferSec
			}
			if l.Mode == Ride && l.Dep < need {
				return fmt.Errorf("leg %d boards at %d, before it can (%d)", i, l.Dep, need)
			}
			if l.Mode == Walk && l.Dep != prev.Arr {
				return fmt.Errorf("walk leg %d starts at %d, previous leg ended at %d", i, l.Dep, prev.Arr)
			}
			if prev.Mode == Walk && l.Mode == Walk {
				return fmt.Errorf("two walks in a row at leg %d", i)
			}
		}
		if l.Mode == Ride {
			rides++
			if len(l.Stops) < 2 || l.Stops[0] != l.From || l.Stops[len(l.Stops)-1] != l.To {
				return fmt.Errorf("ride leg %d has inconsistent stops", i)
			}
			if !tripExists(n.TT, l) {
				return fmt.Errorf("ride leg %d does not match any trip in the timetable", i)
			}
		}
	}
	if rides != j.Rides {
		return fmt.Errorf("journey says %d rides, legs have %d", j.Rides, rides)
	}
	return nil
}
