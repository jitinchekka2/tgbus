package planapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jitinchekka2/tgbus/index"
	"github.com/jitinchekka2/tgbus/raptor"
	"github.com/jitinchekka2/tgbus/timetable"
)

// fixture loads the small test feed: stops A (Koti), B (Abids), C (Uppal),
// D (Dilsukhnagar); route 10A runs A-B-C at 08:00 and returns C-B-A at 09:00.
func fixture(t *testing.T) *Service {
	t.Helper()
	idx, err := index.Load("../testdata/feed")
	if err != nil {
		t.Fatal(err)
	}
	return New(timetable.Build(idx), raptor.DefaultOptions())
}

func TestPlan(t *testing.T) {
	s := fixture(t)
	at := func(h, m int) int32 { return int32(h*3600 + m*60) }
	tests := []struct {
		name     string
		req      Request
		wantCode string
		wantErr  string
		check    func(t *testing.T, r Response)
	}{
		{
			name: "ride from Koti to Uppal",
			req:  Request{FromLat: 17.385, FromLon: 78.486, ToLat: 17.400, ToLon: 78.560, Depart: at(7, 50)},
			check: func(t *testing.T, r Response) {
				if len(r.Journeys) != 1 {
					t.Fatalf("journeys = %d, want 1", len(r.Journeys))
				}
				j := r.Journeys[0]
				if j.Rides != 1 || j.Arrive != at(8, 30) || len(j.Legs) != 3 {
					t.Fatalf("journey = %+v", j)
				}
				walkIn, ride, walkOut := j.Legs[0], j.Legs[1], j.Legs[2]
				if walkIn.Mode != "walk" || walkIn.From != nil || walkIn.To == nil || walkIn.To.ID != "A" {
					t.Errorf("first leg = %+v, want a walk from the origin point to stop A", walkIn)
				}
				if ride.Mode != "ride" || ride.Route == nil || ride.Route.Short != "10A" || ride.Dep != at(8, 0) {
					t.Errorf("ride leg = %+v", ride)
				}
				if got := len(ride.Stops); got != 3 || ride.Stops[1].ID != "B" {
					t.Errorf("ride stops = %+v, want A,B,C", ride.Stops)
				}
				if walkOut.Mode != "walk" || walkOut.To != nil || walkOut.From == nil || walkOut.From.ID != "C" {
					t.Errorf("last leg = %+v, want a walk from stop C to the destination point", walkOut)
				}
			},
		},
		{
			name: "the same trip the other way uses the later bus",
			req:  Request{FromLat: 17.400, FromLon: 78.560, ToLat: 17.385, ToLon: 78.486, Depart: at(8, 30)},
			check: func(t *testing.T, r Response) {
				if len(r.Journeys) != 1 || r.Journeys[0].Arrive != at(9, 30) {
					t.Fatalf("journeys = %+v", r.Journeys)
				}
			},
		},
		{
			// The fixture has a trip leaving Koti at 25:10 (01:10 the next morning).
			name: "late evening catches the past-midnight trip",
			req:  Request{FromLat: 17.385, FromLon: 78.486, ToLat: 17.400, ToLon: 78.560, Depart: at(23, 0)},
			check: func(t *testing.T, r Response) {
				if len(r.Journeys) != 1 || r.Journeys[0].Arrive != at(25, 40) {
					t.Fatalf("journeys = %+v", r.Journeys)
				}
			},
		},
		{
			name:     "origin far from any stop",
			req:      Request{FromLat: 18.5, FromLon: 79.5, ToLat: 17.400, ToLon: 78.560, Depart: at(8, 0)},
			wantCode: "no_stops", wantErr: "walking distance",
		},
		{
			name:     "latitude out of range",
			req:      Request{FromLat: 91, FromLon: 78, ToLat: 17.4, ToLon: 78.5, Depart: at(8, 0)},
			wantCode: "bad_request", wantErr: "coordinates",
		},
		{
			name:     "departure is not a time of day",
			req:      Request{FromLat: 17.385, FromLon: 78.486, ToLat: 17.4, ToLon: 78.56, Depart: 86400},
			wantCode: "bad_request", wantErr: "time of day",
		},
		{
			name:     "negative departure",
			req:      Request{FromLat: 17.385, FromLon: 78.486, ToLat: 17.4, ToLon: 78.56, Depart: -1},
			wantCode: "bad_request", wantErr: "time of day",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := s.Plan(tc.req)
			if r.Journeys == nil {
				t.Error("Journeys must be an empty list, not null, so JavaScript can iterate it")
			}
			if r.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q (error %q)", r.Code, tc.wantCode, r.Error)
			}
			if tc.wantErr != "" && !strings.Contains(r.Error, tc.wantErr) {
				t.Errorf("error = %q, want containing %q", r.Error, tc.wantErr)
			}
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
}

func TestPlanJSON(t *testing.T) {
	s := fixture(t)
	tests := []struct {
		name     string
		in       string
		wantCode string
		wantRide bool
	}{
		{name: "valid request", in: `{"fromLat":17.385,"fromLon":78.486,"toLat":17.4,"toLon":78.56,"depart":28200}`, wantRide: true},
		{name: "malformed JSON", in: `{"fromLat":`, wantCode: "bad_request"},
		{name: "wrong type", in: `{"fromLat":"north"}`, wantCode: "bad_request"},
		{name: "empty object: every field is required", in: `{}`, wantCode: "bad_request"},
		{name: "one field missing", in: `{"fromLat":17.385,"fromLon":78.486,"toLat":17.4,"toLon":78.56}`, wantCode: "bad_request"},
		{name: "zero coordinates are allowed when given explicitly", in: `{"fromLat":0,"fromLon":0,"toLat":17.4,"toLon":78.56,"depart":0}`, wantCode: "no_stops"},
		{name: "empty string", in: ``, wantCode: "bad_request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r Response
			if err := json.Unmarshal([]byte(s.PlanJSON(tc.in)), &r); err != nil {
				t.Fatalf("response is not JSON: %v", err)
			}
			if r.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", r.Code, tc.wantCode)
			}
			if tc.wantRide && (len(r.Journeys) != 1 || r.Journeys[0].Rides != 1) {
				t.Errorf("journeys = %+v", r.Journeys)
			}
		})
	}
}

func TestJSONShape(t *testing.T) {
	// The page depends on these exact field names.
	s := fixture(t)
	out := s.PlanJSON(`{"fromLat":17.385,"fromLon":78.486,"toLat":17.4,"toLon":78.56,"depart":28200}`)
	for _, want := range []string{
		`"journeys":[`, `"depart":`, `"arrive":`, `"rides":1`, `"legs":[`, `"mode":"walk"`, `"mode":"ride"`,
		`"route":{"id":"R1","short":"10A"`, `"stops":[{"id":"A"`, `"from":null`, `"to":null`, `"dep":`, `"arr":`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("response missing %s\n%s", want, out)
		}
	}
	if strings.Contains(out, `"error"`) {
		t.Errorf("a successful response must not carry an error: %s", out)
	}
}

func TestLoad(t *testing.T) {
	idx, err := index.Load("../testdata/feed")
	if err != nil {
		t.Fatal(err)
	}
	good, err := timetable.Encode(timetable.Build(idx))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{name: "valid", data: good},
		{name: "empty", data: nil, wantErr: true},
		{name: "truncated", data: good[:len(good)/2], wantErr: true},
		{name: "garbage", data: []byte("this is not a timetable"), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Load(tc.data, raptor.DefaultOptions())
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil {
				if st, rt := s.Counts(); st != 4 || rt != 2 {
					t.Errorf("counts = %d stops, %d routes; want 4, 2", st, rt)
				}
			}
		})
	}
}
