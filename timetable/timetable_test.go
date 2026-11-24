package timetable

import (
	"encoding/binary"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/jitinchekka2/tgbus/index"
)

// synthetic builds an index by hand so grouping rules can be tested directly.
func synthetic() *index.Index {
	stops := []*index.Stop{
		{ID: "A", Name: "Alpha", Lat: 17.1234567, Lon: 78.7654321},
		{ID: "B", Name: "Beta", Lat: 17.2, Lon: 78.8},
		{ID: "C", Name: "Gamma, with comma", Lat: 17.3, Lon: 78.9},
	}
	r1 := &index.Route{ID: "R1", Short: "1", Long: "1 - Alpha to Gamma"}
	r2 := &index.Route{ID: "R2", Short: "2", Long: "2 - Same stops, other route"}
	unused := &index.Route{ID: "R3", Short: "3", Long: "3 - no trips"}
	return &index.Index{
		Stops:  stops,
		Routes: []*index.Route{r1, r2, unused},
		Trips: []index.TripCalls{
			// Same pattern; first and third share run times, second differs. Out of order on purpose.
			{Route: r1, Stop: []int32{0, 1, 2}, Dep: []int32{36000, 36600, 37200}},
			{Route: r1, Stop: []int32{0, 1, 2}, Dep: []int32{28800, 29500, 30400}},
			{Route: r1, Stop: []int32{0, 1, 2}, Dep: []int32{32400, 33000, 33600}},
			// Same stops on a different route is a different pattern.
			{Route: r2, Stop: []int32{0, 1, 2}, Dep: []int32{40000, 40100, 40200}},
			// Reverse direction, and a past-midnight trip.
			{Route: r1, Stop: []int32{2, 1, 0}, Dep: []int32{90000, 90600, 91200}},
		},
	}
}

func TestBuildGrouping(t *testing.T) {
	tt := Build(synthetic())

	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "unused routes are dropped", got: len(tt.Routes), want: 2},
		{name: "route order follows the index", got: []string{tt.Routes[0].ID, tt.Routes[1].ID}, want: []string{"R1", "R2"}},
		{name: "patterns: forward, other route, reverse", got: len(tt.Patterns), want: 3},
		{name: "trips of the first pattern", got: len(tt.Patterns[0].Trips), want: 3},
		{name: "trips are sorted by departure", got: []int32{tt.Patterns[0].Trips[0].Dep0, tt.Patterns[0].Trips[1].Dep0, tt.Patterns[0].Trips[2].Dep0}, want: []int32{28800, 32400, 36000}},
		{name: "run times are shared between identical trips", got: len(tt.Patterns[0].Profiles), want: 2},
		{name: "equal profiles map to the same index", got: tt.Patterns[0].Trips[1].Profile == tt.Patterns[0].Trips[2].Profile, want: true},
		{name: "profile offsets start at zero", got: tt.Patterns[0].Profiles[0], want: []int32{0, 700, 1600}},
		{name: "single-profile pattern", got: len(tt.Patterns[1].Profiles), want: 1},
		{name: "past-midnight departure kept", got: tt.Patterns[2].Trips[0].Dep0, want: int32(90000)},
		{name: "coordinates are rounded to 1e-6 degrees", got: [2]float64{tt.Stops[0].Lat, tt.Stops[0].Lon}, want: [2]float64{17.123457, 78.765432}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
	if err := tt.Validate(); err != nil {
		t.Errorf("built timetable is invalid: %v", err)
	}
	p, tr, calls := tt.Counts()
	if p != 3 || tr != 5 || calls != 15 {
		t.Errorf("counts = %d/%d/%d, want 3/5/15", p, tr, calls)
	}
}

func TestBuildFixtureFeed(t *testing.T) {
	idx, err := index.Load("../testdata/feed")
	if err != nil {
		t.Fatal(err)
	}
	tt := Build(idx)
	if err := tt.Validate(); err != nil {
		t.Fatal(err)
	}
	p, tr, _ := tt.Counts()
	// R1: A-B-C, A-C and C-B-A are three patterns; R2: A-D. R3 has no trips.
	if p != 4 || tr != 4 || len(tt.Routes) != 2 {
		t.Errorf("patterns/trips/routes = %d/%d/%d, want 4/4/2", p, tr, len(tt.Routes))
	}
}

func TestRoundTrip(t *testing.T) {
	idx, err := index.Load("../testdata/feed")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		tt   *Timetable
	}{
		{name: "synthetic with shared and distinct profiles", tt: Build(synthetic())},
		{name: "fixture feed", tt: Build(idx)},
		{name: "empty network", tt: &Timetable{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Encode(tc.tt)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			// nil and empty slices are equivalent for an empty network.
			if len(tc.tt.Patterns) == 0 && len(got.Patterns) == 0 && len(got.Stops) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.tt) {
				t.Errorf("round trip changed the timetable\n got: %+v\nwant: %+v", got, tc.tt)
			}
		})
	}
}

func TestEncodeRejectsInvalid(t *testing.T) {
	valid := func() *Timetable { return Build(synthetic()) }
	tests := []struct {
		name    string
		mutate  func(*Timetable)
		wantErr string
	}{
		{name: "route out of range", mutate: func(t *Timetable) { t.Patterns[0].Route = 99 }, wantErr: "route"},
		{name: "stop out of range", mutate: func(t *Timetable) { t.Patterns[0].Stops[1] = 99 }, wantErr: "stop"},
		{name: "one stop only", mutate: func(t *Timetable) { t.Patterns[0].Stops = t.Patterns[0].Stops[:1] }, wantErr: "at least two"},
		{name: "profile does not start at zero", mutate: func(t *Timetable) { t.Patterns[0].Profiles[0][0] = 5 }, wantErr: "bad shape"},
		{name: "times decrease", mutate: func(t *Timetable) { t.Patterns[0].Profiles[0][2] = 1 }, wantErr: "decrease"},
		{name: "profile index out of range", mutate: func(t *Timetable) { t.Patterns[0].Trips[0].Profile = 9 }, wantErr: "profile"},
		{name: "trips unsorted", mutate: func(t *Timetable) { t.Patterns[0].Trips[0].Dep0 = 99999 }, wantErr: "not sorted"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tt := valid()
			tc.mutate(tt)
			_, err := Encode(tt)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// craft builds a tiny file by hand so Decode's checks can be hit precisely.
func craft(stopIdx, routeIdx uint64, nStops uint64) []byte {
	b := []byte{'T', 'G', 'B', 'T', 1}
	b = binary.AppendUvarint(b, 2) // two stops
	for _, id := range []string{"A", "B"} {
		b = binary.AppendUvarint(b, uint64(len(id)))
		b = append(b, id...)
		b = binary.AppendUvarint(b, 0) // empty name
		b = binary.LittleEndian.AppendUint32(b, 0)
		b = binary.LittleEndian.AppendUint32(b, 0)
	}
	b = binary.AppendUvarint(b, 1) // one route
	for i := 0; i < 3; i++ {
		b = binary.AppendUvarint(b, 0)
	}
	b = binary.AppendUvarint(b, 1) // one pattern
	b = binary.AppendUvarint(b, routeIdx)
	b = binary.AppendUvarint(b, nStops)
	b = binary.AppendUvarint(b, 0)
	b = binary.AppendUvarint(b, stopIdx)
	b = binary.AppendUvarint(b, 1) // one profile
	b = binary.AppendUvarint(b, 60)
	b = binary.AppendUvarint(b, 1) // one trip
	b = binary.AppendUvarint(b, 100)
	return b
}

func TestDecodeErrors(t *testing.T) {
	good, err := Encode(Build(synthetic()))
	if err != nil {
		t.Fatal(err)
	}
	badVersion := append([]byte(nil), good...)
	badVersion[4] = 9
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{name: "nil", data: nil, wantErr: "bad header"},
		{name: "wrong magic", data: []byte("NOPE\x01\x00"), wantErr: "bad header"},
		{name: "unsupported version", data: badVersion, wantErr: "version"},
		{name: "trailing bytes", data: append(append([]byte(nil), good...), 0), wantErr: "trailing"},
		{name: "route index out of range", data: craft(1, 7, 2), wantErr: "route index"},
		{name: "stop index out of range", data: craft(5, 0, 2), wantErr: "stop index"},
		{name: "pattern with one stop", data: craft(0, 0, 1), wantErr: "1 stops"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
	// The crafted file with valid indexes must decode, proving the cases above fail for their stated reason.
	if _, err := Decode(craft(1, 0, 2)); err != nil {
		t.Fatalf("crafted valid file rejected: %v", err)
	}
}

func TestDecodeTruncatedNeverPanics(t *testing.T) {
	good, err := Encode(Build(synthetic()))
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(good); n++ {
		if _, err := Decode(good[:n]); err == nil {
			t.Fatalf("truncated to %d of %d bytes decoded without error", n, len(good))
		}
	}
}

func TestDecodeCorruptionNeverPanics(t *testing.T) {
	good, err := Encode(Build(synthetic()))
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 5000; i++ {
		data := append([]byte(nil), good...)
		for k := 0; k < 1+rng.Intn(4); k++ {
			data[rng.Intn(len(data))] = byte(rng.Intn(256))
		}
		tt, err := Decode(data) // must not panic
		if err == nil && tt.Validate() != nil {
			t.Fatalf("Decode accepted a timetable that fails Validate (iteration %d)", i)
		}
	}
}
