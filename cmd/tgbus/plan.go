package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jitinchekka2/tgbus/index"
	"github.com/jitinchekka2/tgbus/raptor"
	"github.com/jitinchekka2/tgbus/timetable"
)

// parseLatLon parses "lat,lon".
func parseLatLon(s string) (raptor.Point, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return raptor.Point{}, fmt.Errorf("%q: want lat,lon", s)
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return raptor.Point{}, fmt.Errorf("%q: not a valid latitude,longitude", s)
	}
	return raptor.Point{Lat: lat, Lon: lon}, nil
}

// parseClock parses "H:MM" or "HH:MM" into seconds since midnight.
func parseClock(s string) (int32, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if !ok || err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("%q: want a time like 08:30", s)
	}
	return int32(hh*3600 + mm*60), nil
}

// findStop picks a stop by name: an exact match first, then a name that starts
// with the query, then one that contains it (all case-insensitive).
func findStop(tt *timetable.Timetable, query string) (int, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return -1, errors.New("empty stop name")
	}
	best, bestScore := -1, 4
	for i, s := range tt.Stops {
		name := strings.ToLower(s.Name)
		score := 4
		switch {
		case name == q:
			score = 0
		case strings.HasPrefix(name, q):
			score = 1
		case strings.Contains(name, q):
			score = 2
		}
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return -1, fmt.Errorf("no stop matches %q", query)
	}
	return best, nil
}

// clockText renders seconds since midnight as HH:MM, marking the next day.
func clockText(sec int32) string {
	d := ""
	if sec >= 86400 {
		d = " (+1 day)"
		sec -= 86400
	}
	return fmt.Sprintf("%02d:%02d%s", sec/3600, sec%3600/60, d)
}

func minutes(sec int32) int32 { return (sec + 30) / 60 }

func routeLabel(r timetable.Route) string {
	if r.Short == "" || r.Short == r.ID {
		return r.ID
	}
	return r.Short
}

func printJourneys(w io.Writer, tt *timetable.Timetable, js []raptor.Journey) {
	for i, j := range js {
		_, _ = fmt.Fprintf(w, "Option %d: %d ride(s), leave %s, arrive %s (%d min)\n",
			i+1, j.Rides, clockText(j.Depart), clockText(j.Arrive), minutes(j.Arrive-j.Depart))
		for _, l := range j.Legs {
			switch l.Mode {
			case raptor.Walk:
				from, to := "start", "destination"
				if l.From >= 0 {
					from = tt.Stops[l.From].Name
				}
				if l.To >= 0 {
					to = tt.Stops[l.To].Name
				}
				_, _ = fmt.Fprintf(w, "  %s  walk ~%d m (%d min) from %s to %s\n", clockText(l.Dep), l.WalkM, minutes(l.Arr-l.Dep), from, to)
			case raptor.Ride:
				r := tt.Routes[l.Route]
				_, _ = fmt.Fprintf(w, "  %s  bus %s: %s -> %s (%d stops), arrive %s\n",
					clockText(l.Dep), routeLabel(r), tt.Stops[l.From].Name, tt.Stops[l.To].Name, len(l.Stops)-1, clockText(l.Arr))
			}
		}
	}
}

func cmdPlan(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	feed := fs.String("feed", defaultFeed, "directory containing the GTFS .txt files")
	ttFile := fs.String("timetable", "", "read a built timetable.bin instead of the feed")
	from := fs.String("from", "", "origin as lat,lon")
	to := fs.String("to", "", "destination as lat,lon")
	fromStop := fs.String("from-stop", "", "origin as a stop name (instead of -from)")
	toStop := fs.String("to-stop", "", "destination as a stop name (instead of -to)")
	at := fs.String("at", "", "departure time of day, HH:MM (default: now, India time)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	start := time.Now()
	var tt *timetable.Timetable
	if *ttFile != "" {
		b, err := os.ReadFile(*ttFile)
		if err != nil {
			return err
		}
		if tt, err = timetable.Decode(b); err != nil {
			return err
		}
	} else {
		idx, err := index.Load(*feed)
		if err != nil {
			return fmt.Errorf("load feed: %w", err)
		}
		tt = timetable.Build(idx)
	}
	net := raptor.New(tt, raptor.DefaultOptions())
	prepared := time.Since(start)

	resolve := func(point, stop, what string) (raptor.Point, error) {
		switch {
		case point != "" && stop != "":
			return raptor.Point{}, fmt.Errorf("give either -%s or -%s-stop, not both", what, what)
		case point != "":
			return parseLatLon(point)
		case stop != "":
			i, err := findStop(tt, stop)
			if err != nil {
				return raptor.Point{}, err
			}
			s := tt.Stops[i]
			_, _ = fmt.Fprintf(stdout, "%s: %s (%.5f,%.5f)\n", what, s.Name, s.Lat, s.Lon)
			return raptor.Point{Lat: s.Lat, Lon: s.Lon}, nil
		}
		return raptor.Point{}, fmt.Errorf("give -%s or -%s-stop", what, what)
	}
	origin, err := resolve(*from, *fromStop, "from")
	if err != nil {
		return err
	}
	dest, err := resolve(*to, *toStop, "to")
	if err != nil {
		return err
	}

	var depart int32
	if *at == "" {
		ist, locErr := time.LoadLocation("Asia/Kolkata")
		if locErr != nil {
			ist = time.FixedZone("IST", 5*3600+1800)
		}
		n := time.Now().In(ist)
		depart = int32(n.Hour()*3600 + n.Minute()*60)
	} else if depart, err = parseClock(*at); err != nil {
		return err
	}

	t0 := time.Now()
	js, err := net.Plan(origin, dest, depart)
	took := time.Since(t0)
	if errors.Is(err, raptor.ErrNoStops) {
		_, _ = fmt.Fprintln(stdout, "No bus stop within walking distance of the origin or destination.")
		return nil
	}
	if err != nil {
		return err
	}
	if len(js) == 0 {
		_, _ = fmt.Fprintf(stdout, "No journey found leaving at %s.\n", clockText(depart))
	} else {
		printJourneys(stdout, tt, js)
	}
	_, _ = fmt.Fprintf(stdout, "(prepared %d stops, %d routes in %s; planned in %s)\n",
		len(tt.Stops), len(tt.Routes), prepared.Round(time.Millisecond), took.Round(time.Microsecond))
	return nil
}
