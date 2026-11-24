package gtfs

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseTime converts a GTFS time ("H:MM:SS" or "HH:MM:SS") into seconds since
// the start of the service day. Hours may exceed 23 for trips that run past
// midnight, e.g. "25:10:00".
func ParseTime(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid GTFS time %q", s)
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid GTFS time %q", s)
		}
		v[i] = n
	}
	if v[1] > 59 || v[2] > 59 {
		return 0, fmt.Errorf("invalid GTFS time %q", s)
	}
	return v[0]*3600 + v[1]*60 + v[2], nil
}
