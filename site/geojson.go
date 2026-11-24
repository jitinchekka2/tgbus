package site

import "github.com/jitinchekka2/tgbus/index"

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

func newCollection() featureCollection {
	return featureCollection{Type: "FeatureCollection", Features: []feature{}}
}
