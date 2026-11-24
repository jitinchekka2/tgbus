// Package planapi is the JSON-friendly face of the journey planner. The
// browser build (cmd/planner-wasm) is a thin wrapper around it, which keeps
// the request validation and response shape testable without a browser.
package planapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jitinchekka2/tgbus/raptor"
	"github.com/jitinchekka2/tgbus/timetable"
)

// Request is a journey query. Depart is seconds since midnight, India time.
type Request struct {
	FromLat float64 `json:"fromLat"`
	FromLon float64 `json:"fromLon"`
	ToLat   float64 `json:"toLat"`
	ToLon   float64 `json:"toLon"`
	Depart  int32   `json:"depart"`
}

// Stop is a stop on a journey.
type Stop struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// Route identifies the bus route of a ride.
type Route struct {
	ID    string `json:"id"`
	Short string `json:"short"`
	Long  string `json:"long"`
}

// Leg is one step: "walk" or "ride". A walk's From or To is nil when it is
// the query's origin or destination point. Times are seconds since midnight
// and run past 86400 after midnight.
type Leg struct {
	Mode  string `json:"mode"`
	Route *Route `json:"route,omitempty"`
	From  *Stop  `json:"from"`
	To    *Stop  `json:"to"`
	Dep   int32  `json:"dep"`
	Arr   int32  `json:"arr"`
	Stops []Stop `json:"stops,omitempty"` // rides: every stop from boarding to alighting
	WalkM int32  `json:"walkM,omitempty"`
}

// Journey is one itinerary.
type Journey struct {
	Depart int32 `json:"depart"`
	Arrive int32 `json:"arrive"`
	Rides  int   `json:"rides"`
	Legs   []Leg `json:"legs"`
}

// Response is the answer to a Request. Code is a short machine-readable
// reason when there are no journeys ("no_stops", "no_journey") or the request was bad ("bad_request").
type Response struct {
	Journeys []Journey `json:"journeys"`
	Code     string    `json:"code,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Service plans journeys on one network. It is not safe for concurrent use.
type Service struct {
	net *raptor.Network
	tt  *timetable.Timetable
}

// New prepares a Service for tt.
func New(tt *timetable.Timetable, opts raptor.Options) *Service {
	return &Service{net: raptor.New(tt, opts), tt: tt}
}

// Load decodes a timetable.bin and prepares a Service for it.
func Load(data []byte, opts raptor.Options) (*Service, error) {
	tt, err := timetable.Decode(data)
	if err != nil {
		return nil, err
	}
	return New(tt, opts), nil
}

// Counts reports the size of the loaded network.
func (s *Service) Counts() (stops, routes int) { return len(s.tt.Stops), len(s.tt.Routes) }

func validPoint(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// Plan answers a query. It never returns a Go error: problems are reported in
// the Response so they can cross the JavaScript boundary as data.
func (s *Service) Plan(req Request) Response {
	if !validPoint(req.FromLat, req.FromLon) || !validPoint(req.ToLat, req.ToLon) {
		return Response{Journeys: []Journey{}, Code: "bad_request", Error: "origin and destination must be valid coordinates"}
	}
	if req.Depart < 0 || req.Depart >= 86400 {
		return Response{Journeys: []Journey{}, Code: "bad_request", Error: "departure must be a time of day between 00:00 and 23:59"}
	}
	js, err := s.net.Plan(raptor.Point{Lat: req.FromLat, Lon: req.FromLon}, raptor.Point{Lat: req.ToLat, Lon: req.ToLon}, req.Depart)
	if errors.Is(err, raptor.ErrNoStops) {
		return Response{Journeys: []Journey{}, Code: "no_stops", Error: "no bus stop within walking distance of the origin or destination"}
	}
	if err != nil {
		return Response{Journeys: []Journey{}, Code: "bad_request", Error: err.Error()}
	}
	out := Response{Journeys: make([]Journey, 0, len(js))}
	for _, j := range js {
		out.Journeys = append(out.Journeys, s.convert(j))
	}
	if len(out.Journeys) == 0 {
		out.Code = "no_journey"
		out.Error = "no journey found at that time"
	}
	return out
}

// rawRequest has pointer fields so a missing field can be told from a zero
// (latitude 0 is a real place; an absent latitude is a mistake).
type rawRequest struct {
	FromLat, FromLon, ToLat, ToLon *float64
	Depart                         *int32
}

func (r *rawRequest) UnmarshalJSON(b []byte) error {
	var m struct {
		FromLat *float64 `json:"fromLat"`
		FromLon *float64 `json:"fromLon"`
		ToLat   *float64 `json:"toLat"`
		ToLon   *float64 `json:"toLon"`
		Depart  *int32   `json:"depart"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*r = rawRequest{m.FromLat, m.FromLon, m.ToLat, m.ToLon, m.Depart}
	return nil
}

// PlanJSON is Plan for callers that exchange JSON text. Every field of
// Request is required.
func (s *Service) PlanJSON(request string) string {
	var raw rawRequest
	var resp Response
	switch err := json.Unmarshal([]byte(request), &raw); {
	case err != nil:
		resp = Response{Journeys: []Journey{}, Code: "bad_request", Error: fmt.Sprintf("invalid request: %v", err)}
	case raw.FromLat == nil || raw.FromLon == nil || raw.ToLat == nil || raw.ToLon == nil || raw.Depart == nil:
		resp = Response{Journeys: []Journey{}, Code: "bad_request", Error: "invalid request: fromLat, fromLon, toLat, toLon and depart are all required"}
	default:
		resp = s.Plan(Request{FromLat: *raw.FromLat, FromLon: *raw.FromLon, ToLat: *raw.ToLat, ToLon: *raw.ToLon, Depart: *raw.Depart})
	}
	b, err := json.Marshal(resp)
	if err != nil { // cannot happen for these types; keep the boundary total anyway
		return `{"journeys":[],"code":"internal","error":"could not encode the response"}`
	}
	return string(b)
}

func (s *Service) stop(i int32) *Stop {
	if i < 0 {
		return nil
	}
	st := s.tt.Stops[i]
	return &Stop{ID: st.ID, Name: st.Name, Lat: st.Lat, Lon: st.Lon}
}

func (s *Service) convert(j raptor.Journey) Journey {
	out := Journey{Depart: j.Depart, Arrive: j.Arrive, Rides: j.Rides, Legs: make([]Leg, 0, len(j.Legs))}
	for _, l := range j.Legs {
		leg := Leg{Dep: l.Dep, Arr: l.Arr, From: s.stop(l.From), To: s.stop(l.To), WalkM: l.WalkM}
		if l.Mode == raptor.Ride {
			r := s.tt.Routes[l.Route]
			leg.Mode = "ride"
			leg.Route = &Route{ID: r.ID, Short: r.Short, Long: r.Long}
			for _, si := range l.Stops {
				leg.Stops = append(leg.Stops, *s.stop(si))
			}
		} else {
			leg.Mode = "walk"
		}
		out.Legs = append(out.Legs, leg)
	}
	return out
}
