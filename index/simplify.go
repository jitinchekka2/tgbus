package index

import "math"

// Coord is a [longitude, latitude] pair, the order GeoJSON uses.
type Coord [2]float64

const (
	metresPerDegLat = 110540.0
	metresPerDegLon = 111320.0
)

// Simplify reduces a polyline with the Douglas–Peucker algorithm. tolMetres is
// the largest distance a dropped vertex may lie from the simplified line. The
// first and last vertices are always kept. Inputs with fewer than three points
// are returned as a copy.
func Simplify(pts []Coord, tolMetres float64) []Coord {
	n := len(pts)
	if n < 3 || tolMetres <= 0 {
		return append([]Coord(nil), pts...)
	}

	// Project to local metres. Telangana is small, so one cos(lat) factor
	// taken at the first point is accurate enough for a drawing tolerance.
	kx := metresPerDegLon * math.Cos(pts[0][1]*math.Pi/180)
	xy := make([][2]float64, n)
	for i, p := range pts {
		xy[i] = [2]float64{p[0] * kx, p[1] * metresPerDegLat}
	}

	keep := make([]bool, n)
	keep[0], keep[n-1] = true, true
	stack := [][2]int{{0, n - 1}}
	tol2 := tolMetres * tolMetres
	for len(stack) > 0 {
		r := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		first, last := r[0], r[1]
		maxD, idx := 0.0, -1
		for i := first + 1; i < last; i++ {
			if d := segDist2(xy[i], xy[first], xy[last]); d > maxD {
				maxD, idx = d, i
			}
		}
		if idx >= 0 && maxD > tol2 {
			keep[idx] = true
			stack = append(stack, [2]int{first, idx}, [2]int{idx, last})
		}
	}

	out := make([]Coord, 0, n/4+2)
	for i, k := range keep {
		if k {
			out = append(out, pts[i])
		}
	}
	return out
}

// segDist2 is the squared distance from p to the segment ab. Using the
// segment rather than the infinite line keeps looping routes intact.
func segDist2(p, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / l2
		t = math.Max(0, math.Min(1, t))
	}
	ex, ey := a[0]+t*dx-p[0], a[1]+t*dy-p[1]
	return ex*ex + ey*ey
}

// RoundCoords rounds to 5 decimal places (~1 m) and drops consecutive
// duplicates created by the rounding.
func RoundCoords(pts []Coord) []Coord {
	out := make([]Coord, 0, len(pts))
	for _, p := range pts {
		c := Coord{math.Round(p[0]*1e5) / 1e5, math.Round(p[1]*1e5) / 1e5}
		if len(out) > 0 && out[len(out)-1] == c {
			continue
		}
		out = append(out, c)
	}
	return out
}
