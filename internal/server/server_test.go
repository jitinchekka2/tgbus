package server

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jitinchekka2/tgbus/index"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	idx, err := index.Load("../../testdata/feed")
	if err != nil {
		t.Fatalf("index.Load: %v", err)
	}
	static := fstest.MapFS{"index.html": {Data: []byte("<h1>map</h1>")}}
	return New(idx, static)
}

func get(t *testing.T, h http.Handler, target string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestParseBBox(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    [4]float64
		wantErr bool
	}{
		{name: "valid", in: "78,17,79,18", want: [4]float64{78, 17, 79, 18}},
		{name: "spaces and decimals", in: " 78.5, 17.25,79,18 ", want: [4]float64{78.5, 17.25, 79, 18}},
		{name: "too few values", in: "78,17,79", wantErr: true},
		{name: "not a number", in: "a,17,79,18", wantErr: true},
		{name: "west greater than east", in: "79,17,78,18", wantErr: true},
		{name: "south greater than north", in: "78,18,79,17", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, s, e, n, err := parseBBox(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && [4]float64{w, s, e, n} != tt.want {
				t.Errorf("got %v, want %v", [4]float64{w, s, e, n}, tt.want)
			}
		})
	}
}

func TestStopsEndpoint(t *testing.T) {
	h := newTestHandler(t)
	tests := []struct {
		name      string
		url       string
		wantCode  int
		wantCount int
	}{
		{name: "all stops", url: "/api/stops", wantCode: 200, wantCount: 4},
		// Only Uppal (78.56,17.40) lies east of 78.55.
		{name: "bbox filter", url: "/api/stops?bbox=78.55,17,79,18", wantCode: 200, wantCount: 1},
		{name: "empty bbox result", url: "/api/stops?bbox=0,0,1,1", wantCode: 200, wantCount: 0},
		{name: "bad bbox", url: "/api/stops?bbox=oops", wantCode: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, tt.url)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantCode, rec.Body)
			}
			if tt.wantCode != 200 {
				return
			}
			var fc struct {
				Features []struct {
					Properties map[string]any `json:"properties"`
				} `json:"features"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
				t.Fatal(err)
			}
			if len(fc.Features) != tt.wantCount {
				t.Errorf("features = %d, want %d", len(fc.Features), tt.wantCount)
			}
		})
	}
}

func TestStopEndpoint(t *testing.T) {
	h := newTestHandler(t)
	tests := []struct {
		name       string
		url        string
		wantCode   int
		wantTrips  float64
		wantRoutes int
	}{
		{name: "stop on two routes", url: "/api/stops/A", wantCode: 200, wantTrips: 4, wantRoutes: 2},
		{name: "unknown stop", url: "/api/stops/NOPE", wantCode: 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, tt.url)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode != 200 {
				return
			}
			var got struct {
				Trips  float64 `json:"trips"`
				Routes []struct {
					ID string `json:"id"`
				} `json:"routes"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Trips != tt.wantTrips || len(got.Routes) != tt.wantRoutes {
				t.Errorf("trips/routes = %v/%d, want %v/%d", got.Trips, len(got.Routes), tt.wantTrips, tt.wantRoutes)
			}
		})
	}
}

func TestSearchEndpoint(t *testing.T) {
	h := newTestHandler(t)
	tests := []struct {
		name    string
		url     string
		wantIDs []string
	}{
		{name: "by short name", url: "/api/routes?q=10a", wantIDs: []string{"R1"}},
		{name: "by long name", url: "/api/routes?q=beta", wantIDs: []string{"R2"}},
		{name: "no query", url: "/api/routes", wantIDs: []string{}},
		{name: "no match", url: "/api/routes?q=zzz", wantIDs: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, tt.url)
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
			var got []struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("got %d results, want %d", len(got), len(tt.wantIDs))
			}
			for i, id := range tt.wantIDs {
				if got[i].ID != id {
					t.Errorf("result %d = %s, want %s", i, got[i].ID, id)
				}
			}
		})
	}
}

func TestRouteEndpoint(t *testing.T) {
	h := newTestHandler(t)
	tests := []struct {
		name                string
		url                 string
		wantCode            int
		wantLines, wantStop int
		wantFirst, wantLast string
	}{
		{name: "two lines", url: "/api/routes/R1", wantCode: 200, wantLines: 2, wantStop: 6, wantFirst: "08:00", wantLast: "25:10"},
		{name: "route without trips", url: "/api/routes/R3", wantCode: 200, wantFirst: "", wantLast: ""},
		{name: "unknown route", url: "/api/routes/NOPE", wantCode: 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, tt.url)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode != 200 {
				return
			}
			var got struct {
				Features []struct {
					Properties map[string]any `json:"properties"`
				} `json:"features"`
				Route routeInfo `json:"route"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			lines, stops := 0, 0
			for _, f := range got.Features {
				switch f.Properties["kind"] {
				case "line":
					lines++
				case "stop":
					stops++
				}
			}
			if lines != tt.wantLines || stops != tt.wantStop {
				t.Errorf("lines/stops = %d/%d, want %d/%d", lines, stops, tt.wantLines, tt.wantStop)
			}
			if got.Route.FirstDep != tt.wantFirst || got.Route.LastDep != tt.wantLast {
				t.Errorf("first/last = %q/%q, want %q/%q", got.Route.FirstDep, got.Route.LastDep, tt.wantFirst, tt.wantLast)
			}
		})
	}
}

func TestOverviewEndpoint(t *testing.T) {
	h := newTestHandler(t)
	tests := []struct {
		name     string
		hdr      []string
		wantGzip bool
	}{
		{name: "plain", wantGzip: false},
		{name: "gzip accepted", hdr: []string{"Accept-Encoding", "gzip, deflate"}, wantGzip: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, "/api/overview", tt.hdr...)
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
			if gz := rec.Header().Get("Content-Encoding") == "gzip"; gz != tt.wantGzip {
				t.Fatalf("gzip = %v, want %v", gz, tt.wantGzip)
			}
			var body io.Reader = rec.Body
			if tt.wantGzip {
				zr, err := gzip.NewReader(rec.Body)
				if err != nil {
					t.Fatal(err)
				}
				body = zr
			}
			var fc struct {
				Features []struct {
					Properties map[string]string `json:"properties"`
				} `json:"features"`
			}
			if err := json.NewDecoder(body).Decode(&fc); err != nil {
				t.Fatal(err)
			}
			// R1 has two lines and R2 one; R3 has none.
			if len(fc.Features) != 3 {
				t.Errorf("features = %d, want 3", len(fc.Features))
			}
		})
	}
}

func TestStaticAndRouting(t *testing.T) {
	h := newTestHandler(t)
	tests := []struct {
		name     string
		method   string
		url      string
		wantCode int
		wantBody string
	}{
		{name: "index page", method: "GET", url: "/", wantCode: 200, wantBody: "<h1>map</h1>"},
		{name: "missing static file", method: "GET", url: "/nope.js", wantCode: 404},
		{name: "POST not allowed on API", method: "POST", url: "/api/stops", wantCode: 405},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.url, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want containing %q", rec.Body, tt.wantBody)
			}
		})
	}
}
