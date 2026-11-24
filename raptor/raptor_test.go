package raptor

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"

	"github.com/jitinchekka2/tgbus/timetable"
)

func opts0() Options {
	o := DefaultOptions()
	o.MinTransferSec = 0
	o.MaxRides = 20
	o.HorizonSec = 3 * 86400
	return o
}

// stopAcc is a walk of zero seconds to a stop, so tests can talk in stops.
func stopAcc(s int32) []Access { return []Access{{Stop: s}} }

// line: stops 0..3 spread far apart (no walking between them); route 0 runs
// 0-1-2-3 at 10:00 and 11:00, 10 minutes per hop.
func line() *timetable.Timetable {
	stops := []timetable.Stop{stopAt("A", 0, 0), stopAt("B", 20, 0), stopAt("C", 40, 0), stopAt("D", 60, 0)}
	return build(stops, 1, []pat{{route: 0, stops: []int32{0, 1, 2, 3}, trips: []trip{
		{hm(10, 0), offsets(600, 600, 600)}, {hm(11, 0), offsets(600, 600, 600)},
	}}})
}

func TestPlanBasics(t *testing.T) {
	tests := []struct {
		name           string
		tt             *timetable.Timetable
		opts           func() Options
		from, to       int32
		depart         int32
		wantNone       bool
		wantArrive     int32
		wantRides      int
		wantFirstBoard int32 // departure of the first ride leg, when set
	}{
		{name: "direct trip catches the next bus", tt: line(), opts: opts0, from: 0, to: 3, depart: hm(9, 30), wantArrive: hm(10, 30), wantRides: 1, wantFirstBoard: hm(10, 0)},
		{name: "leaving just after a bus waits for the next one", tt: line(), opts: opts0, from: 0, to: 3, depart: hm(10, 1), wantArrive: hm(11, 30), wantRides: 1, wantFirstBoard: hm(11, 0)},
		{name: "boarding in the middle of the line", tt: line(), opts: opts0, from: 1, to: 3, depart: hm(10, 0), wantArrive: hm(10, 30), wantRides: 1},
		{name: "wrong direction is unreachable on the same day", tt: line(), opts: opts0, from: 3, to: 0, depart: hm(9, 0), wantNone: true},
		{
			name: "after the last bus, tomorrow's first bus is used", tt: line(), opts: opts0, from: 0, to: 1,
			depart: hm(23, 0), wantArrive: 86400 + hm(10, 10), wantRides: 1, wantFirstBoard: 86400 + hm(10, 0),
		},
		{
			name: "horizon stops the search from using tomorrow's bus", tt: line(),
			opts: func() Options { o := opts0(); o.HorizonSec = 3600; return o },
			from: 0, to: 1, depart: hm(23, 0), wantNone: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := New(tc.tt, tc.opts())
			js := n.PlanAccess(stopAcc(tc.from), stopAcc(tc.to), tc.depart, nil)
			if tc.wantNone {
				if len(js) != 0 {
					t.Fatalf("expected no journey, got %+v", js)
				}
				return
			}
			if len(js) == 0 {
				t.Fatal("expected a journey, got none")
			}
			j := js[len(js)-1]
			if j.Arrive != tc.wantArrive || j.Rides != tc.wantRides {
				t.Errorf("arrive/rides = %d/%d, want %d/%d", j.Arrive, j.Rides, tc.wantArrive, tc.wantRides)
			}
			if err := checkJourney(n, j, tc.depart); err != nil {
				t.Error(err)
			}
			if tc.wantFirstBoard != 0 {
				for _, l := range j.Legs {
					if l.Mode == Ride {
						if l.Dep != tc.wantFirstBoard {
							t.Errorf("boards at %d, want %d", l.Dep, tc.wantFirstBoard)
						}
						break
					}
				}
			}
		})
	}
}

func TestPastMidnightTrips(t *testing.T) {
	// A trip leaving at 25:00 (01:00 the next morning) is also available at
	// 01:00 on the query day because the service runs every day.
	stops := []timetable.Stop{stopAt("A", 0, 0), stopAt("B", 20, 0)}
	tt := build(stops, 1, []pat{{route: 0, stops: []int32{0, 1}, trips: []trip{{hm(25, 0), offsets(600)}}}})
	n := New(tt, opts0())
	tests := []struct {
		name       string
		depart     int32
		wantArrive int32
	}{
		{name: "00:30 catches last night's 25:00 trip at 01:00", depart: hm(0, 30), wantArrive: hm(1, 10)},
		{name: "00:59 still catches it", depart: hm(0, 59), wantArrive: hm(1, 10)},
		{name: "01:01 misses it and takes the next one a day later", depart: hm(1, 1), wantArrive: 86400 + hm(1, 10)},
		{name: "evening waits for the 25:00 trip of the same day", depart: hm(20, 0), wantArrive: hm(25, 10)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			js := n.PlanAccess(stopAcc(0), stopAcc(1), tc.depart, nil)
			if len(js) == 0 {
				t.Fatal("no journey")
			}
			if got := js[len(js)-1].Arrive; got != tc.wantArrive {
				t.Errorf("arrive = %d, want %d", got, tc.wantArrive)
			}
			if err := checkJourney(n, js[len(js)-1], tc.depart); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestTransfers(t *testing.T) {
	// Route 0: A-B (arrives B 10:10). Route 1: B-C leaving 10:11 and 10:30.
	// Route 2: B2-C where B2 is a different stop 200 m from B, leaving 10:20.
	stops := []timetable.Stop{
		stopAt("A", 0, 0), stopAt("B", 20, 0), stopAt("C", 40, 0), stopAt("B2", 22, 0), stopAt("Far", 20, 8),
	}
	tt := build(stops, 3, []pat{
		{route: 0, stops: []int32{0, 1}, trips: []trip{{hm(10, 0), offsets(600)}}},
		{route: 1, stops: []int32{1, 2}, trips: []trip{{hm(10, 11), offsets(600)}, {hm(10, 30), offsets(600)}}},
		{route: 2, stops: []int32{3, 2}, trips: []trip{{hm(10, 20), offsets(300)}}},
	})
	tests := []struct {
		name       string
		slack      int32
		wantArrive int32
		wantRides  int
	}{
		{name: "no buffer: the 10:11 connection is made", slack: 0, wantArrive: hm(10, 21), wantRides: 2},
		{name: "two-minute buffer: 10:11 is missed, the walk to B2 and its 10:20 bus wins", slack: 120, wantArrive: hm(10, 25), wantRides: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := opts0()
			o.MinTransferSec = tc.slack
			n := New(tt, o)
			js := n.PlanAccess(stopAcc(0), stopAcc(2), hm(9, 55), nil)
			if len(js) == 0 {
				t.Fatal("no journey")
			}
			j := js[len(js)-1]
			if j.Arrive != tc.wantArrive || j.Rides != tc.wantRides {
				t.Errorf("arrive/rides = %d/%d, want %d/%d (%+v)", j.Arrive, j.Rides, tc.wantArrive, tc.wantRides, j.Legs)
			}
			if err := checkJourney(n, j, hm(9, 55)); err != nil {
				t.Error(err)
			}
		})
	}

	t.Run("a far stop is not a walking transfer", func(t *testing.T) {
		n := New(tt, opts0())
		for _, f := range n.transfers[1] {
			if f.to == 4 {
				t.Errorf("stop 800 m away must not be a transfer: %+v", n.transfers[1])
			}
		}
		var near []int32
		for _, f := range n.transfers[1] {
			near = append(near, f.to)
		}
		if !reflect.DeepEqual(near, []int32{3}) {
			t.Errorf("transfers from B = %v, want [3] (B2 only)", near)
		}
	})
}

func TestOvertaking(t *testing.T) {
	// Same stops: a slow bus leaving at 10:00 and an express leaving at 10:05
	// that reaches the far end first. The planner must take the express.
	stops := []timetable.Stop{stopAt("A", 0, 0), stopAt("B", 20, 0), stopAt("C", 40, 0)}
	tt := build(stops, 1, []pat{{route: 0, stops: []int32{0, 1, 2}, trips: []trip{
		{hm(10, 0), offsets(1200, 1200)},
		{hm(10, 5), offsets(300, 300)},
	}}})
	n := New(tt, opts0())
	if got := n.NumClasses(); got < 2 {
		t.Errorf("an overtaking pair must be split into separate classes, got %d", got)
	}
	js := n.PlanAccess(stopAcc(0), stopAcc(2), hm(9, 50), nil)
	if len(js) != 1 {
		t.Fatalf("journeys = %d, want 1", len(js))
	}
	if want := hm(10, 15); js[0].Arrive != want {
		t.Errorf("arrive = %d, want %d (the express)", js[0].Arrive, want)
	}
	if err := checkJourney(n, js[0], hm(9, 50)); err != nil {
		t.Error(err)
	}
}

func TestParetoByRides(t *testing.T) {
	// Direct bus is slow (arrives 11:00). Two buses with a transfer arrive at
	// 10:30. Both must be offered: fewer rides first, then the faster one.
	stops := []timetable.Stop{stopAt("A", 0, 0), stopAt("M", 20, 0), stopAt("Z", 40, 0)}
	tt := build(stops, 3, []pat{
		{route: 0, stops: []int32{0, 2}, trips: []trip{{hm(10, 0), offsets(3600)}}},
		{route: 1, stops: []int32{0, 1}, trips: []trip{{hm(10, 0), offsets(600)}}},
		{route: 2, stops: []int32{1, 2}, trips: []trip{{hm(10, 15), offsets(900)}}},
	})
	n := New(tt, opts0())
	js := n.PlanAccess(stopAcc(0), stopAcc(2), hm(9, 55), nil)
	if len(js) != 2 {
		t.Fatalf("journeys = %d, want 2: %+v", len(js), js)
	}
	got := [][2]int32{{int32(js[0].Rides), js[0].Arrive}, {int32(js[1].Rides), js[1].Arrive}}
	want := [][2]int32{{1, hm(11, 0)}, {2, hm(10, 30)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("(rides, arrive) = %v, want %v", got, want)
	}
	for _, j := range js {
		if err := checkJourney(n, j, hm(9, 55)); err != nil {
			t.Error(err)
		}
	}
}

func TestPlanFromPoints(t *testing.T) {
	stops := []timetable.Stop{stopAt("A", 0, 0), stopAt("B", 60, 0), stopAt("C", 120, 0)}
	tt := build(stops, 1, []pat{{route: 0, stops: []int32{0, 1, 2}, trips: []trip{{hm(10, 0), offsets(600, 600)}}}})
	n := New(tt, DefaultOptions())
	at := func(x, y float64) Point { s := stopAt("", x, y); return Point{s.Lat, s.Lon} }

	t.Run("walks to the first stop, rides, walks from the last", func(t *testing.T) {
		js, err := n.Plan(at(-2, 0), at(122, 0), hm(9, 50))
		if err != nil {
			t.Fatal(err)
		}
		if len(js) != 1 {
			t.Fatalf("journeys = %d, want 1", len(js))
		}
		j := js[0]
		if j.Rides != 1 || len(j.Legs) != 3 || j.Legs[0].Mode != Walk || j.Legs[1].Mode != Ride || j.Legs[2].Mode != Walk {
			t.Fatalf("unexpected shape: %+v", j.Legs)
		}
		if j.Legs[1].Dep != hm(10, 0) || j.Legs[1].From != 0 || j.Legs[1].To != 2 {
			t.Errorf("ride leg = %+v", j.Legs[1])
		}
		if j.Arrive <= hm(10, 20) {
			t.Errorf("arrival %d must include the final walk after 10:20", j.Arrive)
		}
		if err := checkJourney(n, j, hm(9, 50)); err != nil {
			t.Error(err)
		}
	})
	t.Run("very close points are a walk only", func(t *testing.T) {
		js, err := n.Plan(at(0, 0), at(3, 0), hm(9, 50))
		if err != nil {
			t.Fatal(err)
		}
		if len(js) == 0 || js[0].Rides != 0 || len(js[0].Legs) != 1 || js[0].Legs[0].Mode != Walk {
			t.Fatalf("want a single walk leg first, got %+v", js)
		}
	})
	t.Run("a point far from every stop has no route", func(t *testing.T) {
		if _, err := n.Plan(at(0, 500), at(120, 0), hm(9, 50)); !errors.Is(err, ErrNoStops) {
			t.Errorf("error = %v, want ErrNoStops", err)
		}
	})
}

func TestMaxRidesLimit(t *testing.T) {
	// A chain of five single-hop routes needs five rides. Each bus leaves ten
	// minutes after the previous one so every connection can be made.
	var stops []timetable.Stop
	var pats []pat
	for i := 0; i < 6; i++ {
		stops = append(stops, stopAt(string(rune('A'+i)), float64(i*20), 0))
	}
	for i := 0; i < 5; i++ {
		pats = append(pats, pat{route: int32(i), stops: []int32{int32(i), int32(i + 1)}, trips: []trip{{hm(10, 10*i), offsets(300)}}})
	}
	tt := build(stops, 5, pats)
	tests := []struct {
		name     string
		maxRides int
		wantNone bool
	}{
		{name: "limit of four rides cannot reach", maxRides: 4, wantNone: true},
		{name: "limit of five rides can", maxRides: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := opts0()
			o.MaxRides = tc.maxRides
			n := New(tt, o)
			js := n.PlanAccess(stopAcc(0), stopAcc(5), hm(9, 50), nil)
			if tc.wantNone != (len(js) == 0) {
				t.Fatalf("journeys = %+v, wantNone = %v", js, tc.wantNone)
			}
			if !tc.wantNone && js[len(js)-1].Rides != 5 {
				t.Errorf("rides = %d, want 5", js[len(js)-1].Rides)
			}
		})
	}
}

// randomNetwork builds a small random timetable that exercises overtaking,
// loops, past-midnight trips and walking transfers.
func randomNetwork(rng *rand.Rand) *timetable.Timetable {
	nStops := 6 + rng.Intn(10)
	var stops []timetable.Stop
	for i := 0; i < nStops; i++ {
		// A 6x6 grid of ~100 m cells: many stop pairs are within walking transfer range.
		stops = append(stops, stopAt(string(rune('a'+i)), float64(rng.Intn(6)*3), float64(rng.Intn(6)*3)))
	}
	nRoutes := 2 + rng.Intn(5)
	var pats []pat
	for p := 0; p < nRoutes+rng.Intn(3); p++ {
		ns := 2 + rng.Intn(5)
		seq := make([]int32, 0, ns)
		for len(seq) < ns {
			s := int32(rng.Intn(nStops))
			if len(seq) > 0 && seq[len(seq)-1] == s {
				continue // consecutive repeats are meaningless; loops may revisit later
			}
			seq = append(seq, s)
		}
		var trips []trip
		for k := 0; k < 1+rng.Intn(6); k++ {
			hops := make([]int32, ns-1)
			for i := range hops {
				hops[i] = int32(60 + rng.Intn(900))
			}
			trips = append(trips, trip{dep0: int32(rng.Intn(100000)), offsets: offsets(hops...)})
		}
		pats = append(pats, pat{route: int32(p % nRoutes), stops: seq, trips: trips})
	}
	return build(stops, nRoutes, pats)
}

// randomAccess picks up to three stops that are not in avoid. Origin and
// destination stop sets are kept disjoint: a shared stop is reached by walking
// rather than riding (see PlanAccess), which the oracle does not model.
func randomAccess(rng *rand.Rand, nStops int, avoid []Access) []Access {
	skip := map[int32]bool{}
	for _, a := range avoid {
		skip[a.Stop] = true
	}
	var out []Access
	for k := 0; k < 1+rng.Intn(3); k++ {
		s := int32(rng.Intn(nStops))
		if !skip[s] {
			skip[s] = true
			out = append(out, Access{Stop: s, Sec: int32(rng.Intn(600))})
		}
	}
	return out
}

// TestAgainstConnectionScan checks the earliest arrival of RAPTOR against a
// different algorithm on many random networks and queries, and validates
// every journey it returns.
func TestAgainstConnectionScan(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	queries, reached := 0, 0
	for netNo := 0; netNo < 400; netNo++ {
		tt := randomNetwork(rng)
		if err := tt.Validate(); err != nil {
			t.Fatalf("random network is invalid: %v", err)
		}
		o := opts0()
		if rng.Intn(2) == 0 {
			o.MinTransferSec = 120
		}
		n := New(tt, o)
		conns := connections(tt)
		for q := 0; q < 15; q++ {
			origins := randomAccess(rng, len(tt.Stops), nil)
			dests := randomAccess(rng, len(tt.Stops), origins)
			if len(dests) == 0 {
				continue
			}
			depart := int32(rng.Intn(86400))
			js := n.PlanAccess(origins, dests, depart, nil)
			want := csa(n, conns, origins, dests, depart, depart+o.HorizonSec)

			got := inf
			if len(js) > 0 {
				got = js[len(js)-1].Arrive
			}
			queries++
			if want != inf {
				reached++
			}
			if got != want {
				t.Fatalf("network %d query %d: RAPTOR arrives %d, Connection Scan %d\norigins %+v dests %+v depart %d slack %d\njourneys %+v",
					netNo, q, got, want, origins, dests, depart, o.MinTransferSec, js)
			}
			for i, j := range js {
				if err := checkJourney(n, j, depart); err != nil {
					t.Fatalf("network %d query %d journey %d: %v\n%+v", netNo, q, i, err, j)
				}
				if i > 0 && (j.Rides <= js[i-1].Rides || j.Arrive >= js[i-1].Arrive) {
					t.Fatalf("network %d query %d: journeys are not a Pareto set: %+v", netNo, q, js)
				}
			}
		}
	}
	// Guard against a vacuous pass: most random queries should actually reach the destination.
	if reached < queries/4 {
		t.Fatalf("only %d of %d random queries were reachable; the generator is too sparse to test anything", reached, queries)
	}
	t.Logf("%d queries over 400 random networks, %d reachable, all matched", queries, reached)
}

func TestSharedStopIsWalkedNotRidden(t *testing.T) {
	// Origin and destination both walk to stop A. Riding away and back to A
	// can never beat the walk, so no journey with rides is offered through A.
	n := New(line(), opts0())
	js := n.PlanAccess([]Access{{Stop: 0, Sec: 60}}, []Access{{Stop: 0, Sec: 60}}, hm(9, 0), nil)
	if len(js) != 0 {
		t.Errorf("journeys = %+v, want none", js)
	}
	direct := &Journey{Depart: hm(9, 0), Arrive: hm(9, 2), Legs: []Leg{{Mode: Walk, Route: -1, From: -1, To: -1, Dep: hm(9, 0), Arr: hm(9, 2)}}}
	js = n.PlanAccess([]Access{{Stop: 0, Sec: 60}}, []Access{{Stop: 0, Sec: 60}}, hm(9, 0), direct)
	if len(js) != 1 || js[0].Rides != 0 {
		t.Errorf("with a direct walk offered, journeys = %+v, want just the walk", js)
	}
}
