package main

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/jitinchekka2/tgbus/raptor"
	"github.com/jitinchekka2/tgbus/timetable"
)

func TestParseLatLon(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    raptor.Point
		wantErr bool
	}{
		{name: "typical", in: "17.385,78.486", want: raptor.Point{Lat: 17.385, Lon: 78.486}},
		{name: "spaces around parts", in: " 17.385 , 78.486 ", want: raptor.Point{Lat: 17.385, Lon: 78.486}},
		{name: "negative", in: "-33.9,-70.5", want: raptor.Point{Lat: -33.9, Lon: -70.5}},
		{name: "one number", in: "17.385", wantErr: true},
		{name: "three numbers", in: "1,2,3", wantErr: true},
		{name: "not numbers", in: "north,east", wantErr: true},
		{name: "latitude out of range", in: "91,78", wantErr: true},
		{name: "longitude out of range", in: "17,181", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLatLon(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (math.Abs(got.Lat-tt.want.Lat) > 1e-9 || math.Abs(got.Lon-tt.want.Lon) > 1e-9) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseClock(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int32
		wantErr bool
	}{
		{name: "morning", in: "08:30", want: 8*3600 + 30*60},
		{name: "single-digit hour", in: "7:05", want: 7*3600 + 5*60},
		{name: "midnight", in: "00:00", want: 0},
		{name: "last minute", in: "23:59", want: 23*3600 + 59*60},
		{name: "hour out of range", in: "24:00", wantErr: true},
		{name: "minute out of range", in: "12:60", wantErr: true},
		{name: "no colon", in: "0830", wantErr: true},
		{name: "text", in: "noon", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseClock(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFindStop(t *testing.T) {
	tt := &timetable.Timetable{Stops: []timetable.Stop{
		{ID: "1", Name: "Koti Bus Stand"},
		{ID: "2", Name: "Koti"},
		{ID: "3", Name: "Women's College Koti"},
		{ID: "4", Name: "Uppal X Roads"},
	}}
	tests := []struct {
		name    string
		query   string
		want    int
		wantErr bool
	}{
		{name: "exact match beats a prefix match", query: "koti", want: 1},
		{name: "case-insensitive", query: "KOTI", want: 1},
		{name: "prefix", query: "koti bus", want: 0},
		{name: "substring", query: "college", want: 2},
		{name: "surrounding spaces ignored", query: "  uppal  ", want: 3},
		{name: "no match", query: "nowhere", wantErr: true},
		{name: "empty", query: " ", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := findStop(tt, tc.query)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("got stop %d, want %d", got, tc.want)
			}
		})
	}
}

func TestClockText(t *testing.T) {
	tests := []struct {
		in   int32
		want string
	}{
		{in: 0, want: "00:00"},
		{in: 8*3600 + 5*60, want: "08:05"},
		{in: 86399, want: "23:59"},
		{in: 86400 + 70*60, want: "01:10 (+1 day)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := clockText(tt.in); got != tt.want {
				t.Errorf("clockText(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPlanCommand(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErr    string
		wantStdout []string
	}{
		{
			name: "ride between named stops",
			args: []string{"plan", "-feed", "../../testdata/feed", "-from-stop", "Koti", "-to-stop", "Uppal", "-at", "07:50"},
			wantStdout: []string{
				"Option 1: 1 ride(s), leave 07:50, arrive 08:30 (40 min)",
				"bus 10A: Koti, Women's College -> Uppal (2 stops), arrive 08:30",
			},
		},
		{
			name:       "coordinates work too",
			args:       []string{"plan", "-feed", "../../testdata/feed", "-from", "17.385,78.486", "-to", "17.4,78.56", "-at", "07:50"},
			wantStdout: []string{"Option 1:", "bus 10A"},
		},
		{
			name:       "a place far from every stop",
			args:       []string{"plan", "-feed", "../../testdata/feed", "-from", "28.6,77.2", "-to", "17.4,78.56", "-at", "07:50"},
			wantStdout: []string{"No bus stop within walking distance"},
		},
		{
			name:    "origin and destination both required",
			args:    []string{"plan", "-feed", "../../testdata/feed", "-to-stop", "Uppal"},
			wantErr: "give -from or -from-stop",
		},
		{
			name:    "cannot give a point and a stop for the same end",
			args:    []string{"plan", "-feed", "../../testdata/feed", "-from", "17,78", "-from-stop", "Koti", "-to-stop", "Uppal"},
			wantErr: "either -from or -from-stop",
		},
		{
			name:    "unknown stop name",
			args:    []string{"plan", "-feed", "../../testdata/feed", "-from-stop", "Atlantis", "-to-stop", "Uppal"},
			wantErr: "no stop matches",
		},
		{
			name:    "bad time",
			args:    []string{"plan", "-feed", "../../testdata/feed", "-from-stop", "Koti", "-to-stop", "Uppal", "-at", "25:00"},
			wantErr: "want a time like",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing %q; got:\n%s", want, stdout.String())
				}
			}
		})
	}
}
