package raptor

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

const inf = int32(math.MaxInt32)

// Mode says what a leg of a journey is.
type Mode uint8

const (
	// Walk is a leg on foot, between the origin or destination and a stop, or between two stops.
	Walk Mode = iota
	// Ride is a leg on a bus.
	Ride
)

// Leg is one step of a journey. Times are seconds since the start of the
// service day of the query and can exceed 86400 after midnight.
type Leg struct {
	Mode Mode
	// Route indexes Timetable.Routes for a Ride; -1 for a Walk.
	Route int32
	// From and To are stop indexes, or -1 for the query's origin/destination point.
	From, To int32
	Dep, Arr int32
	// Stops lists the stops of a Ride from boarding to alighting inclusive.
	Stops []int32
	// WalkM is the estimated walking distance in metres for a Walk.
	WalkM int32
}

// Journey is one itinerary. Journeys returned by a query are the best for
// each number of rides: more rides only appear if they arrive earlier.
type Journey struct {
	Depart, Arrive int32
	Rides          int
	Legs           []Leg
}

// Access connects the origin or destination point to a stop on foot.
type Access struct {
	Stop int32
	Sec  int32
	M    int32
}

// ErrNoStops is returned when no stop lies within walking range of a point.
var ErrNoStops = errors.New("no bus stop within walking distance")

// Point is a latitude/longitude pair.
type Point struct{ Lat, Lon float64 }

// accessStops finds the nearest stops within walking range of p.
func (n *Network) accessStops(p Point) []Access {
	near := n.grid.near(p.Lat, p.Lon, n.opts.MaxAccessM)
	if len(near) > n.opts.MaxAccessStops {
		near = near[:n.opts.MaxAccessStops]
	}
	out := make([]Access, len(near))
	for i, nb := range near {
		out[i] = Access{Stop: nb.Stop, Sec: n.opts.walkSec(nb.DistM), M: int32(math.Round(nb.DistM * n.opts.Detour))}
	}
	return out
}

// Plan finds journeys from one point to another leaving at depart (seconds
// since midnight). It returns the best journey for each number of rides, in
// order of increasing rides and decreasing arrival time.
func (n *Network) Plan(from, to Point, depart int32) ([]Journey, error) {
	origins := n.accessStops(from)
	dests := n.accessStops(to)
	var direct *Journey
	if d := distM(from.Lat, from.Lon, to.Lat, to.Lon); d <= n.opts.MaxDirectWalkM {
		sec := n.opts.walkSec(d)
		direct = &Journey{
			Depart: depart, Arrive: depart + sec,
			Legs: []Leg{{Mode: Walk, Route: -1, From: -1, To: -1, Dep: depart, Arr: depart + sec, WalkM: int32(math.Round(d * n.opts.Detour))}},
		}
	}
	if (len(origins) == 0 || len(dests) == 0) && direct == nil {
		return nil, ErrNoStops
	}
	return n.PlanAccess(origins, dests, depart, direct), nil
}

type labelKind uint8

const (
	kNone labelKind = iota
	kAccess
	kRide
	kWalk
)

// label records how a stop was reached in one round and when. Bus and walk
// arrivals are kept apart so a later walk can never overwrite the bus ride a
// walk started from.
type label struct {
	kind  labelKind
	arr   int32
	class int32 // kRide
	trip  int32 // kRide: index into class.refs
	board int32 // kRide: position in the class where the rider got on
	alt   int32 // kRide: position where the rider gets off; kWalk: the stop walked from
}

// search holds the state of one query.
type search struct {
	n    *Network
	arr  [][]int32 // arr[k][s]: earliest arrival at s using at most k rides
	labR [][]label // ride (or initial access) labels improved in round k
	labW [][]label // walk labels improved in round k
}

// find returns the label that produced arrival time t at stop s, looking in
// round r and earlier rounds. Ride labels win over walk labels on a tie.
func (q *search) find(r int, s int32, t int32) (label, int) {
	for ; r >= 0; r-- {
		if l := q.labR[r][s]; l.kind != kNone && l.arr == t {
			return l, r
		}
		if l := q.labW[r][s]; l.kind != kNone && l.arr == t {
			return l, r
		}
	}
	return label{}, -1
}

// improvedIn reports whether round k itself improved the arrival at s.
func (q *search) improvedIn(k int, s int32) bool {
	t := q.arr[k][s]
	return (q.labR[k][s].kind != kNone && q.labR[k][s].arr == t) ||
		(q.labW[k][s].kind != kNone && q.labW[k][s].arr == t)
}

// PlanAccess is Plan with the walking connections to stops already chosen.
// direct, if not nil, is a walk-only journey to offer as the zero-ride option.
//
// A stop that is both an origin and a destination stop is reached by walking
// (the initial walk is earlier than any bus could be), so no journey with
// rides is reported through it; a walk-only journey dominates that case.
func (n *Network) PlanAccess(origins, dests []Access, depart int32, direct *Journey) []Journey {
	nStops := len(n.TT.Stops)
	maxK := n.opts.MaxRides
	horizon := depart + n.opts.HorizonSec

	q := &search{n: n, arr: make([][]int32, maxK+1), labR: make([][]label, maxK+1), labW: make([][]label, maxK+1)}
	for k := 0; k <= maxK; k++ {
		q.arr[k] = make([]int32, nStops)
		q.labR[k] = make([]label, nStops)
		q.labW[k] = make([]label, nStops)
		for s := range q.arr[k] {
			q.arr[k][s] = inf
		}
	}
	accessOf := make(map[int32]Access, len(origins))

	// Two arrival times per stop, because they allow different things: after
	// a bus a rider may walk on to another stop, after a walk they may not.
	// best is the earliest arrival by any means (it limits boarding and
	// walks); bestRide is the earliest by bus only (it limits bus arrivals).
	best := make([]int32, nStops)
	bestRide := make([]int32, nStops)
	for s := range best {
		best[s], bestRide[s] = inf, inf
	}
	marked := make([]bool, nStops)
	var markedList []int32
	mark := func(s int32) {
		if !marked[s] {
			marked[s] = true
			markedList = append(markedList, s)
		}
	}

	var journeys []Journey
	bestDest := inf
	if direct != nil {
		journeys = append(journeys, *direct)
		bestDest = direct.Arrive
	}

	for _, o := range origins {
		t := depart + o.Sec
		if t < q.arr[0][o.Stop] {
			q.arr[0][o.Stop] = t
			best[o.Stop] = t
			q.labR[0][o.Stop] = label{kind: kAccess, arr: t}
			accessOf[o.Stop] = o
			mark(o.Stop)
		}
	}

	// readyAt is when a rider can board at s in round k: the arrival from
	// round k-1 plus the transfer buffer, except right after the initial walk.
	readyAt := func(k int, s int32) int32 {
		t := q.arr[k-1][s]
		if t == inf {
			return inf
		}
		if l, _ := q.find(k-1, s, t); l.kind != kAccess {
			t += n.opts.MinTransferSec
		}
		return t
	}

	for k := 1; k <= maxK && len(markedList) > 0; k++ {
		copy(q.arr[k], q.arr[k-1])

		// Earliest marked position in each class, visited in class order so
		// equal-cost ties resolve the same way every run.
		first := make(map[int32]int32)
		for _, s := range markedList {
			for _, sp := range n.stopClasses[s] {
				if p, ok := first[sp.class]; !ok || sp.pos < p {
					first[sp.class] = sp.pos
				}
			}
			marked[s] = false
		}
		markedList = markedList[:0]
		classes := make([]int32, 0, len(first))
		for ci := range first {
			classes = append(classes, ci)
		}
		sort.Slice(classes, func(i, j int) bool { return classes[i] < classes[j] })

		var improved []int32
		for _, ci := range classes {
			c := &n.classes[ci]
			trip := -1
			board := int32(0)
			for pos := int(first[ci]); pos < c.n; pos++ {
				s := c.stops[pos]
				if trip >= 0 {
					// A bus arrival is worth keeping if it beats the earlier bus
					// arrival, even when walking got here sooner: only the bus
					// arrival can be followed by another walk.
					if t := c.time(trip, pos); t < bestRide[s] && t < bestDest {
						bestRide[s] = t
						q.labR[k][s] = label{kind: kRide, arr: t, class: ci, trip: int32(trip), board: board, alt: int32(pos)}
						improved = append(improved, s)
						if t < best[s] {
							best[s] = t
							q.arr[k][s] = t
							mark(s)
						}
					}
				}
				ready := readyAt(k, s)
				if ready == inf {
					continue
				}
				// Earliest trip leaving this position no sooner than ready.
				nt := len(c.refs)
				cand := sort.Search(nt, func(i int) bool { return c.time(i, pos) >= ready })
				if cand < nt && (trip < 0 || cand < trip) && c.time(cand, pos) <= horizon {
					trip = cand
					board = int32(pos)
				}
			}
		}

		// Walk between nearby stops, starting from each stop's bus arrival.
		// Walks never start from a stop reached by walking.
		sort.Slice(improved, func(i, j int) bool { return improved[i] < improved[j] })
		for _, s := range improved {
			l := q.labR[k][s]
			if l.kind != kRide {
				continue
			}
			for _, f := range n.transfers[s] {
				if t := l.arr + f.sec; t < best[f.to] && t < bestDest {
					q.arr[k][f.to] = t
					best[f.to] = t
					q.labW[k][f.to] = label{kind: kWalk, arr: t, alt: s}
					mark(f.to)
				}
			}
		}

		// Is there a better way to the destination with k rides?
		bestStop := int32(-1)
		var bestEg Access
		cand := bestDest
		for _, e := range dests {
			if q.arr[k][e.Stop] != inf && q.improvedIn(k, e.Stop) {
				if t := q.arr[k][e.Stop] + e.Sec; t < cand {
					cand, bestStop, bestEg = t, e.Stop, e
				}
			}
		}
		if bestStop >= 0 {
			bestDest = cand
			journeys = append(journeys, q.reconstruct(k, bestStop, depart, cand, accessOf, bestEg))
		}
	}
	return journeys
}

// reconstruct walks the labels back from the destination stop to the origin.
func (q *search) reconstruct(k int, stop int32, depart, arrive int32, accessOf map[int32]Access, eg Access) Journey {
	n := q.n
	rev := []Leg{{Mode: Walk, Route: -1, From: stop, To: -1, Dep: arrive - eg.Sec, Arr: arrive, WalkM: eg.M}}

	cur := stop
	round := k
	target := q.arr[k][stop]
	rides := 0
	for {
		l, lr := q.find(round, cur, target)
		switch l.kind {
		case kRide:
			c := &n.classes[l.class]
			board := c.stops[l.board]
			rev = append(rev, Leg{
				Mode: Ride, Route: c.route, From: board, To: cur,
				Dep: c.time(int(l.trip), int(l.board)), Arr: l.arr,
				Stops: append([]int32(nil), c.stops[l.board:l.alt+1]...),
			})
			rides++
			cur, round, target = board, lr-1, q.arr[lr-1][board]
		case kWalk:
			from := l.alt
			fl := q.labR[lr][from] // the bus arrival the walk started from
			a, b := n.TT.Stops[from], n.TT.Stops[cur]
			rev = append(rev, Leg{
				Mode: Walk, Route: -1, From: from, To: cur, Dep: fl.arr, Arr: l.arr,
				WalkM: int32(math.Round(distM(a.Lat, a.Lon, b.Lat, b.Lon) * n.opts.Detour)),
			})
			cur, round, target = from, lr, fl.arr
		case kAccess:
			a := accessOf[cur]
			rev = append(rev, Leg{Mode: Walk, Route: -1, From: -1, To: cur, Dep: depart, Arr: depart + a.Sec, WalkM: a.M})
			legs := make([]Leg, 0, len(rev))
			for i := len(rev) - 1; i >= 0; i-- {
				l := rev[i]
				// A transfer walk followed by the walk to the destination is one walk.
				if p := len(legs) - 1; p >= 0 && l.Mode == Walk && legs[p].Mode == Walk {
					legs[p].To, legs[p].Arr, legs[p].WalkM = l.To, l.Arr, legs[p].WalkM+l.WalkM
					continue
				}
				legs = append(legs, l)
			}
			return Journey{Depart: depart, Arrive: arrive, Rides: rides, Legs: legs}
		default:
			panic(fmt.Sprintf("raptor: broken label chain at stop %d (round %d, time %d)", cur, round, target))
		}
	}
}
