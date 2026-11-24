// Package server exposes the index as a small JSON API plus the static map page.
package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/jitinchekka2/tgbus/index"
)

const searchLimit = 50

// Server serves the API and static files.
type Server struct {
	idx *index.Index

	overviewOnce sync.Once
	overviewRaw  []byte
	overviewGz   []byte
}

// New returns the HTTP handler for idx, serving static files from static.
func New(idx *index.Index, static fs.FS) http.Handler {
	s := &Server{idx: idx}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stops", s.handleStops)
	mux.HandleFunc("GET /api/stops/{id}", s.handleStop)
	mux.HandleFunc("GET /api/routes", s.handleSearch)
	mux.HandleFunc("GET /api/routes/{id}", s.handleRoute)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.Handle("GET /", http.FileServerFS(static))
	return mux
}

// GeoJSON types.

type feature struct {
	Type       string `json:"type"`
	Geometry   any    `json:"geometry"`
	Properties any    `json:"properties"`
}

type featureCollection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

type point struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"`
}

type lineString struct {
	Type        string        `json:"type"`
	Coordinates []index.Coord `json:"coordinates"`
}

func newPoint(lon, lat float64) point { return point{"Point", [2]float64{lon, lat}} }

func newLine(c []index.Coord) lineString { return lineString{"LineString", c} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// parseBBox parses "west,south,east,north".
func parseBBox(s string) (w, so, e, n float64, err error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, fmt.Errorf("bbox must be west,south,east,north")
	}
	var v [4]float64
	for i, p := range parts {
		v[i], err = strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("bbox value %q is not a number", p)
		}
	}
	if v[0] > v[2] || v[1] > v[3] {
		return 0, 0, 0, 0, fmt.Errorf("bbox must have west<=east and south<=north")
	}
	return v[0], v[1], v[2], v[3], nil
}

// GET /api/stops[?bbox=w,s,e,n]
func (s *Server) handleStops(w http.ResponseWriter, r *http.Request) {
	inBox := func(lon, lat float64) bool { return true }
	if q := r.URL.Query().Get("bbox"); q != "" {
		west, south, east, north, err := parseBBox(q)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		inBox = func(lon, lat float64) bool {
			return lon >= west && lon <= east && lat >= south && lat <= north
		}
	}
	fc := featureCollection{Type: "FeatureCollection", Features: []feature{}}
	for _, st := range s.idx.Stops {
		if !inBox(st.Lon, st.Lat) {
			continue
		}
		fc.Features = append(fc.Features, feature{
			Type:     "Feature",
			Geometry: newPoint(st.Lon, st.Lat),
			Properties: map[string]any{
				"id": st.ID, "name": st.Name, "trips": st.Trips, "routes": len(st.Routes),
			},
		})
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, fc)
}

type stopRouteJSON struct {
	ID    string `json:"id"`
	Short string `json:"short"`
	Long  string `json:"long"`
	Trips int    `json:"trips"`
}

// GET /api/stops/{id}
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	st, ok := s.idx.Stop(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "stop not found")
		return
	}
	routes := make([]stopRouteJSON, len(st.Routes))
	for i, sr := range st.Routes {
		routes[i] = stopRouteJSON{sr.Route.ID, sr.Route.Short, sr.Route.Long, sr.Trips}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": st.ID, "name": st.Name, "lat": st.Lat, "lon": st.Lon,
		"trips": st.Trips, "routes": routes,
	})
}

// GET /api/routes?q=
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	found := s.idx.Search(r.URL.Query().Get("q"), searchLimit)
	out := make([]stopRouteJSON, len(found))
	for i, rt := range found {
		out[i] = stopRouteJSON{rt.ID, rt.Short, rt.Long, rt.Trips}
	}
	writeJSON(w, http.StatusOK, out)
}

type routeDetail struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
	Route    routeInfo `json:"route"`
}

type routeInfo struct {
	ID       string `json:"id"`
	Short    string `json:"short"`
	Long     string `json:"long"`
	Trips    int    `json:"trips"`
	FirstDep string `json:"firstDeparture"`
	LastDep  string `json:"lastDeparture"`
}

// GET /api/routes/{id}
func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	rt, ok := s.idx.Route(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	out := routeDetail{
		Type:     "FeatureCollection",
		Features: []feature{},
		Route: routeInfo{
			ID: rt.ID, Short: rt.Short, Long: rt.Long, Trips: rt.Trips,
			FirstDep: index.FormatTime(rt.FirstDep), LastDep: index.FormatTime(rt.LastDep),
		},
	}
	for _, ln := range rt.Lines {
		if len(ln.Coords) >= 2 {
			out.Features = append(out.Features, feature{
				Type:     "Feature",
				Geometry: newLine(ln.Coords),
				Properties: map[string]any{
					"kind": "line", "direction": ln.Direction, "shape": ln.ShapeID, "trips": ln.Trips,
				},
			})
		}
		for i, ls := range ln.Stops {
			out.Features = append(out.Features, feature{
				Type:     "Feature",
				Geometry: newPoint(ls.Stop.Lon, ls.Stop.Lat),
				Properties: map[string]any{
					"kind": "stop", "id": ls.Stop.ID, "name": ls.Stop.Name, "seq": i + 1,
					"time": index.FormatTime(ls.Departure), "direction": ln.Direction,
				},
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/overview: every route's coarse geometry, built once and cached
// both raw and gzipped.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	s.overviewOnce.Do(s.buildOverview)
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "public, max-age=300")
	h.Add("Vary", "Accept-Encoding")
	body := s.overviewRaw
	if len(s.overviewGz) > 0 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		body = s.overviewGz
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if _, err := w.Write(body); err != nil {
		log.Printf("write overview: %v", err)
	}
}

func (s *Server) buildOverview() {
	fc := featureCollection{Type: "FeatureCollection", Features: []feature{}}
	for _, rt := range s.idx.Routes {
		for _, ln := range rt.Lines {
			if len(ln.Overview) < 2 {
				continue
			}
			fc.Features = append(fc.Features, feature{
				Type:       "Feature",
				Geometry:   newLine(ln.Overview),
				Properties: map[string]string{"id": rt.ID, "n": rt.Short},
			})
		}
	}
	raw, err := json.Marshal(fc)
	if err != nil {
		log.Printf("build overview: %v", err)
		raw = []byte(`{"type":"FeatureCollection","features":[]}`)
	}
	s.overviewRaw = raw
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err == nil {
		_, err = zw.Write(raw)
	}
	if err == nil {
		err = zw.Close()
	}
	if err != nil {
		log.Printf("gzip overview: %v (serving uncompressed)", err)
		return
	}
	s.overviewGz = buf.Bytes()
}
