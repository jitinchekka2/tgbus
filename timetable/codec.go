package timetable

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

var magic = [4]byte{'T', 'G', 'B', 'T'}

const (
	version = 1

	// Sanity limits so a corrupt or hostile file cannot make Decode allocate
	// huge slices. They are far above any real network.
	maxCount  = 1 << 24
	maxString = 1 << 16
)

// Encode serialises t. The format is little-endian: a header, then stops,
// routes and patterns. Counts and indexes are unsigned varints; times are
// stored as deltas so most values take one or two bytes.
func Encode(t *Timetable) ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("timetable is invalid: %w", err)
	}
	var b []byte
	b = append(b, magic[:]...)
	b = append(b, version)

	b = binary.AppendUvarint(b, uint64(len(t.Stops)))
	for _, s := range t.Stops {
		b = appendString(b, s.ID)
		b = appendString(b, s.Name)
		b = binary.LittleEndian.AppendUint32(b, uint32(int32(math.Round(s.Lat*1e6))))
		b = binary.LittleEndian.AppendUint32(b, uint32(int32(math.Round(s.Lon*1e6))))
	}
	b = binary.AppendUvarint(b, uint64(len(t.Routes)))
	for _, r := range t.Routes {
		b = appendString(b, r.ID)
		b = appendString(b, r.Short)
		b = appendString(b, r.Long)
	}
	b = binary.AppendUvarint(b, uint64(len(t.Patterns)))
	for _, p := range t.Patterns {
		b = binary.AppendUvarint(b, uint64(p.Route))
		b = binary.AppendUvarint(b, uint64(len(p.Stops)))
		for _, s := range p.Stops {
			b = binary.AppendUvarint(b, uint64(s))
		}
		b = binary.AppendUvarint(b, uint64(len(p.Profiles)))
		for _, pr := range p.Profiles {
			for i := 1; i < len(pr); i++ {
				b = binary.AppendUvarint(b, uint64(pr[i]-pr[i-1]))
			}
		}
		b = binary.AppendUvarint(b, uint64(len(p.Trips)))
		prev := int32(0)
		for _, tr := range p.Trips {
			b = binary.AppendUvarint(b, uint64(tr.Dep0-prev))
			prev = tr.Dep0
			if len(p.Profiles) > 1 {
				b = binary.AppendUvarint(b, uint64(tr.Profile))
			}
		}
	}
	return b, nil
}

func appendString(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

// reader decodes with a sticky error: after the first problem every read
// returns zero values, so callers check err once at the end.
type reader struct {
	b   []byte
	err error
}

func (r *reader) fail(format string, a ...any) {
	if r.err == nil {
		r.err = fmt.Errorf(format, a...)
	}
}

func (r *reader) uv() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.fail("truncated or malformed number")
		return 0
	}
	r.b = r.b[n:]
	return v
}

// count reads a length and checks it against max and the bytes left (each
// item needs at least minBytes), which bounds allocations by the input size.
func (r *reader) count(what string, max uint64, minBytes int) int {
	v := r.uv()
	if r.err != nil {
		return 0
	}
	if v > max || (minBytes > 0 && v > uint64(len(r.b)/minBytes)) {
		r.fail("implausible %s count %d", what, v)
		return 0
	}
	return int(v)
}

// index reads an index that must be below limit.
func (r *reader) index(what string, limit int) int32 {
	v := r.uv()
	if r.err == nil && v >= uint64(limit) {
		r.fail("%s index %d out of range (%d)", what, v, limit)
		return 0
	}
	return int32(v)
}

func (r *reader) str() string {
	n := r.count("string length", maxString, 1)
	if r.err != nil {
		return ""
	}
	s := string(r.b[:n])
	r.b = r.b[n:]
	return s
}

func (r *reader) u32() uint32 {
	if r.err != nil {
		return 0
	}
	if len(r.b) < 4 {
		r.fail("truncated coordinate")
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

// Decode parses data produced by Encode. It returns an error, never panics,
// on malformed input.
func Decode(data []byte) (*Timetable, error) {
	if len(data) < 5 || !bytes.Equal(data[:4], magic[:]) {
		return nil, errors.New("not a tgbus timetable (bad header)")
	}
	if data[4] != version {
		return nil, fmt.Errorf("unsupported timetable version %d (want %d)", data[4], version)
	}
	r := &reader{b: data[5:]}
	t := &Timetable{}

	n := r.count("stop", maxCount, 10)
	t.Stops = make([]Stop, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		s := Stop{ID: r.str(), Name: r.str()}
		s.Lat = float64(int32(r.u32())) / 1e6
		s.Lon = float64(int32(r.u32())) / 1e6
		t.Stops = append(t.Stops, s)
	}
	n = r.count("route", maxCount, 3)
	t.Routes = make([]Route, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		t.Routes = append(t.Routes, Route{ID: r.str(), Short: r.str(), Long: r.str()})
	}
	n = r.count("pattern", maxCount, 5)
	t.Patterns = make([]Pattern, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		p := Pattern{Route: r.index("route", len(t.Routes))}
		ns := r.count("pattern stop", maxCount, 1)
		if r.err == nil && ns < 2 {
			r.fail("pattern %d has %d stops", i, ns)
		}
		p.Stops = make([]int32, 0, ns)
		for j := 0; j < ns && r.err == nil; j++ {
			p.Stops = append(p.Stops, r.index("stop", len(t.Stops)))
		}
		np := r.count("profile", maxCount, ns-1)
		if r.err == nil && np == 0 {
			r.fail("pattern %d has no profiles", i)
		}
		p.Profiles = make([][]int32, 0, np)
		for j := 0; j < np && r.err == nil; j++ {
			pr := make([]int32, ns)
			for k := 1; k < ns && r.err == nil; k++ {
				d := r.uv()
				if d > math.MaxInt32 || int64(pr[k-1])+int64(d) > math.MaxInt32 {
					r.fail("run time overflow")
					break
				}
				pr[k] = pr[k-1] + int32(d)
			}
			p.Profiles = append(p.Profiles, pr)
		}
		nt := r.count("trip", maxCount, 1)
		if r.err == nil && nt == 0 {
			r.fail("pattern %d has no trips", i)
		}
		p.Trips = make([]Trip, 0, nt)
		prev := int64(0)
		for j := 0; j < nt && r.err == nil; j++ {
			d := r.uv()
			prev += int64(d)
			if d > math.MaxInt32 || prev > math.MaxInt32 {
				r.fail("departure overflow")
				break
			}
			tr := Trip{Dep0: int32(prev)}
			if np > 1 {
				tr.Profile = r.index("profile", np)
			}
			p.Trips = append(p.Trips, tr)
		}
		t.Patterns = append(t.Patterns, p)
	}
	if r.err != nil {
		return nil, fmt.Errorf("decode timetable: %w", r.err)
	}
	if len(r.b) != 0 {
		return nil, fmt.Errorf("decode timetable: %d unexpected trailing bytes", len(r.b))
	}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("decode timetable: %w", err)
	}
	return t, nil
}
