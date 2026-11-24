package gtfs

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{name: "midnight", in: "00:00:00", want: 0},
		{name: "morning", in: "03:45:10", want: 3*3600 + 45*60 + 10},
		{name: "single digit hour", in: "9:05:00", want: 9*3600 + 5*60},
		{name: "past midnight", in: "25:10:00", want: 25*3600 + 10*60},
		{name: "surrounding spaces", in: " 08:00:00 ", want: 8 * 3600},
		{name: "empty", in: "", wantErr: true},
		{name: "two parts", in: "08:00", wantErr: true},
		{name: "non numeric", in: "aa:bb:cc", wantErr: true},
		{name: "minutes out of range", in: "08:60:00", wantErr: true},
		{name: "seconds out of range", in: "08:00:60", wantErr: true},
		{name: "negative", in: "-1:00:00", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTime(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTime(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("ParseTime(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestReadCSV(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		required []string
		want     [][]string // values of columns "a" and "b" per row
		wantErr  string
	}{
		{
			name:     "quoted comma and reordered columns",
			input:    "b,a\n\"x, y\",1\nz,2\n",
			required: []string{"a", "b"},
			want:     [][]string{{"1", "x, y"}, {"2", "z"}},
		},
		{
			name:     "BOM and padded header",
			input:    "\ufeffa , b\n1,2\n",
			required: []string{"a", "b"},
			want:     [][]string{{"1", "2"}},
		},
		{
			name:     "short row yields empty value",
			input:    "a,b\n1\n",
			required: []string{"a"},
			want:     [][]string{{"1", ""}},
		},
		{
			name:     "stray quote tolerated",
			input:    "a,b\n5\" wide,2\n",
			required: []string{"a"},
			want:     [][]string{{"5\" wide", "2"}},
		},
		{name: "missing column", input: "a\n1\n", required: []string{"a", "b"}, wantErr: `missing required column "b"`},
		{name: "empty file", input: "", wantErr: "empty file"},
		{name: "header only", input: "a,b\n", required: []string{"a"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got [][]string
			err := ReadCSV(strings.NewReader(tt.input), tt.required, func(r Row) error {
				got = append(got, []string{r.Get("a"), r.Get("b")})
				return nil
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rows = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadCSVCallbackError(t *testing.T) {
	boom := errors.New("boom")
	err := ReadCSV(strings.NewReader("a\n1\n2\n"), nil, func(Row) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
}
