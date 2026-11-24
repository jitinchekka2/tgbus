package raptor

import (
	"math/rand"
	"os"
	"testing"

	"github.com/jitinchekka2/tgbus/index"
	"github.com/jitinchekka2/tgbus/timetable"
)

// TestRealFeedAgainstConnectionScan runs the oracle comparison on a real feed,
// which has loops, overtaking trips and past-midnight service that random
// networks only approximate. It needs the feed, so it is skipped unless
// TGBUS_FEED names a feed directory:
//
//	TGBUS_FEED=path/to/feed go test ./raptor -run RealFeed -v
func TestRealFeedAgainstConnectionScan(t *testing.T) {
	dir := os.Getenv("TGBUS_FEED")
	if dir == "" {
		t.Skip("set TGBUS_FEED to a GTFS feed directory to run this test")
	}
	idx, err := index.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	tt := timetable.Build(idx)
	o := DefaultOptions()
	o.MaxRides = 12
	o.HorizonSec = 36 * 3600
	n := New(tt, o)
	conns := connections(tt)
	t.Logf("%d stops, %d classes (patterns split for overtaking), %d connections", len(tt.Stops), n.NumClasses(), len(conns))

	rng := rand.New(rand.NewSource(7))
	reached, checked := 0, 0
	for q := 0; q < 300; q++ {
		a, b := int32(rng.Intn(len(tt.Stops))), int32(rng.Intn(len(tt.Stops)))
		if a == b {
			continue
		}
		// Spread departures over the whole day, with extra weight on the late-night
		// hours where yesterday's and tomorrow's trips matter.
		depart := int32(rng.Intn(86400))
		if q%4 == 0 {
			depart = int32(22*3600 + rng.Intn(4*3600))
			if depart >= 86400 {
				depart -= 86400
			}
		}
		origins := []Access{{Stop: a, Sec: int32(rng.Intn(300))}}
		dests := []Access{{Stop: b, Sec: int32(rng.Intn(300))}}
		js := n.PlanAccess(origins, dests, depart, nil)
		want := csa(n, conns, origins, dests, depart, depart+o.HorizonSec)
		got := inf
		if len(js) > 0 {
			got = js[len(js)-1].Arrive
		}
		checked++
		if want != inf {
			reached++
		}
		if got != want {
			t.Fatalf("query %d (%s -> %s at %d): RAPTOR %d, Connection Scan %d\n%+v",
				q, tt.Stops[a].Name, tt.Stops[b].Name, depart, got, want, js)
		}
		for i, j := range js {
			if err := checkJourney(n, j, depart); err != nil {
				t.Fatalf("query %d journey %d: %v", q, i, err)
			}
		}
	}
	t.Logf("%d queries, %d reachable: RAPTOR matched Connection Scan on all of them", checked, reached)
}
