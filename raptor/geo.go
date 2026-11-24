package raptor

import (
	"math"
	"sort"
)

const (
	metresPerDegLat = 110540.0
	metresPerDegLon = 111320.0
	cellDeg         = 0.005 // grid cell size, about 550 m
)

// distM is the straight-line distance in metres, accurate enough for walking
// estimates over a city (equirectangular approximation).
func distM(lat1, lon1, lat2, lon2 float64) float64 {
	dy := (lat2 - lat1) * metresPerDegLat
	dx := (lon2 - lon1) * metresPerDegLon * math.Cos((lat1+lat2)/2*math.Pi/180)
	return math.Hypot(dx, dy)
}

type cell struct{ y, x int32 }

func cellOf(lat, lon float64) cell {
	return cell{int32(math.Floor(lat / cellDeg)), int32(math.Floor(lon / cellDeg))}
}

// grid finds stops near a point without scanning them all.
type grid struct {
	cells map[cell][]int32
	lat   []float64
	lon   []float64
}

func newGrid(lat, lon []float64) *grid {
	g := &grid{cells: make(map[cell][]int32), lat: lat, lon: lon}
	for i := range lat {
		c := cellOf(lat[i], lon[i])
		g.cells[c] = append(g.cells[c], int32(i))
	}
	return g
}

// Near is a stop and its straight-line distance from a query point.
type Near struct {
	Stop  int32
	DistM float64
}

// near returns the stops within radiusM of the point, nearest first.
func (g *grid) near(lat, lon, radiusM float64) []Near {
	reachY := int32(math.Ceil(radiusM/metresPerDegLat/cellDeg)) + 1
	cosLat := math.Max(math.Cos(lat*math.Pi/180), 0.01)
	reachX := int32(math.Ceil(radiusM/(metresPerDegLon*cosLat)/cellDeg)) + 1
	c := cellOf(lat, lon)
	var out []Near
	for y := c.y - reachY; y <= c.y+reachY; y++ {
		for x := c.x - reachX; x <= c.x+reachX; x++ {
			for _, s := range g.cells[cell{y, x}] {
				if d := distM(lat, lon, g.lat[s], g.lon[s]); d <= radiusM {
					out = append(out, Near{Stop: s, DistM: d})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DistM != out[j].DistM {
			return out[i].DistM < out[j].DistM
		}
		return out[i].Stop < out[j].Stop
	})
	return out
}
