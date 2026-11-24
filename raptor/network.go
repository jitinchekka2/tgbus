// Package raptor plans bus journeys on a timetable.Timetable with the RAPTOR
// algorithm (Round-bAsed Public Transit Optimized Router): round k finds the
// earliest arrival at every stop using at most k bus rides, so the result is
// the set of best journeys for each number of rides.
//
// The service in this feed runs every day, so a query is a time of day. The
// planner therefore also considers yesterday's trips that run past midnight
// and tomorrow's trips, which lets a 23:30 query catch the first bus next
// morning and a 00:30 query catch the last night bus.
package raptor

import (
	"math"
	"sort"

	"github.com/jitinchekka2/tgbus/timetable"
)

const day = 86400

// Options tune walking and search limits. Use DefaultOptions and change what you need.
type Options struct {
	WalkSpeed      float64 // metres per second
	Detour         float64 // multiplier from straight-line to walking distance
	MaxTransferM   float64 // longest straight-line walk between two stops
	MaxAccessM     float64 // longest straight-line walk from the origin or to the destination
	MaxAccessStops int     // use at most this many nearest stops at each end
	MaxDirectWalkM float64 // offer a walk-only journey up to this straight-line distance
	MinTransferSec int32   // time needed after getting off one bus before boarding another
	MaxRides       int     // most bus rides in one journey
	HorizonSec     int32   // ignore trips departing later than this after the query time
}

// DefaultOptions are sensible values for walking in an Indian city.
func DefaultOptions() Options {
	return Options{
		WalkSpeed:      1.25, // 4.5 km/h
		Detour:         1.3,
		MaxTransferM:   400,
		MaxAccessM:     1000,
		MaxAccessStops: 30,
		MaxDirectWalkM: 1500,
		MinTransferSec: 120,
		MaxRides:       5,
		HorizonSec:     12 * 3600,
	}
}

func (o Options) walkSec(straightM float64) int32 {
	return int32(math.Ceil(straightM * o.Detour / o.WalkSpeed))
}

type tripRef struct {
	pattern, trip int32
	shift         int32 // seconds added to the pattern's times: -86400, 0 or +86400
}

// class is a set of trips of one pattern that never overtake each other, so
// for every stop position the departure times are sorted along the trips.
type class struct {
	route int32
	stops []int32
	n     int
	times []int32 // len(refs)*n, row per trip
	refs  []tripRef
}

func (c *class) time(trip, pos int) int32 { return c.times[trip*c.n+pos] }

type stopPos struct{ class, pos int32 }

type footpath struct {
	to  int32
	sec int32
}

// Network is a timetable prepared for fast queries. It is read-only after
// New, so one Network can serve many queries (but Plan is not safe to call
// concurrently from several goroutines with a shared Network, see Planner).
type Network struct {
	TT   *timetable.Timetable
	opts Options

	classes     []class
	stopClasses [][]stopPos
	transfers   [][]footpath
	grid        *grid
}

// New prepares tt for queries.
func New(tt *timetable.Timetable, opts Options) *Network {
	n := &Network{TT: tt, opts: opts}
	n.buildClasses()
	n.buildStopIndex()
	n.buildTransfers()
	return n
}

func (n *Network) buildClasses() {
	for pi, p := range n.TT.Patterns {
		ns := len(p.Stops)
		type inst struct {
			ref   tripRef
			times []int32
		}
		var insts []inst
		for ti, tr := range p.Trips {
			prof := p.Profiles[tr.Profile]
			last := tr.Dep0 + prof[ns-1]
			for _, shift := range []int32{-day, 0, day} {
				if shift < 0 && last < day {
					continue // only trips running past midnight matter for the previous day
				}
				times := make([]int32, ns)
				for i := range times {
					times[i] = tr.Dep0 + prof[i] + shift
				}
				insts = append(insts, inst{tripRef{int32(pi), int32(ti), shift}, times})
			}
		}
		sort.SliceStable(insts, func(i, j int) bool { return insts[i].times[0] < insts[j].times[0] })

		// Split into non-overtaking classes: a trip joins the first class whose
		// latest trip is nowhere later than it.
		var groups [][]inst
		for _, in := range insts {
			placed := false
			for g := range groups {
				prev := groups[g][len(groups[g])-1].times
				ok := true
				for i := range prev {
					if prev[i] > in.times[i] {
						ok = false
						break
					}
				}
				if ok {
					groups[g] = append(groups[g], in)
					placed = true
					break
				}
			}
			if !placed {
				groups = append(groups, []inst{in})
			}
		}
		for _, g := range groups {
			c := class{route: p.Route, stops: p.Stops, n: ns}
			c.times = make([]int32, 0, len(g)*ns)
			for _, in := range g {
				c.times = append(c.times, in.times...)
				c.refs = append(c.refs, in.ref)
			}
			n.classes = append(n.classes, c)
		}
	}
}

func (n *Network) buildStopIndex() {
	n.stopClasses = make([][]stopPos, len(n.TT.Stops))
	for ci, c := range n.classes {
		for pos, s := range c.stops {
			n.stopClasses[s] = append(n.stopClasses[s], stopPos{int32(ci), int32(pos)})
		}
	}
}

func (n *Network) buildTransfers() {
	lat := make([]float64, len(n.TT.Stops))
	lon := make([]float64, len(n.TT.Stops))
	for i, s := range n.TT.Stops {
		lat[i], lon[i] = s.Lat, s.Lon
	}
	n.grid = newGrid(lat, lon)
	n.transfers = make([][]footpath, len(n.TT.Stops))
	for i := range n.TT.Stops {
		for _, nb := range n.grid.near(lat[i], lon[i], n.opts.MaxTransferM) {
			if int(nb.Stop) != i {
				n.transfers[i] = append(n.transfers[i], footpath{nb.Stop, n.opts.walkSec(nb.DistM)})
			}
		}
	}
}

// NumClasses reports how many non-overtaking trip groups the network has (for diagnostics).
func (n *Network) NumClasses() int { return len(n.classes) }
