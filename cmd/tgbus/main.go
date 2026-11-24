// Command tgbus explores a GTFS bus feed: it can print statistics and serve
// an interactive map of the network.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/jitinchekka2/tgbus/index"
	"github.com/jitinchekka2/tgbus/internal/server"
	"github.com/jitinchekka2/tgbus/web"
)

const defaultFeed = "Telangana_opendata_gtfs_TGSRTC_25_September_2026"

const usage = `tgbus: explore a GTFS bus feed

Usage:
  tgbus serve [-feed dir] [-addr host:port]   serve the interactive map
  tgbus stats [-feed dir]                     print feed statistics and skipped rows

Run "tgbus <command> -h" for a command's flags.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tgbus:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("missing command")
	}
	switch args[0] {
	case "serve":
		return cmdServe(args[1:], stderr)
	case "stats":
		return cmdStats(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	default:
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func loadFeed(dir string) (*index.Index, time.Duration, error) {
	start := time.Now()
	idx, err := index.Load(dir)
	return idx, time.Since(start), err
}

func printStats(w io.Writer, idx *index.Index) {
	st := idx.Stats
	_, _ = fmt.Fprintf(w, "routes:     %d\ntrips:      %d\nstops:      %d\nstop times: %d\nshapes:     %d\n",
		st.Routes, st.Trips, st.Stops, st.StopTimes, st.Shapes)
	reasons := make([]string, 0, len(st.Skipped))
	for r := range st.Skipped {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		_, _ = fmt.Fprintf(w, "skipped %d %s\n", st.Skipped[r], r)
	}
}

func cmdStats(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	feed := fs.String("feed", defaultFeed, "directory containing the GTFS .txt files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	idx, _, err := loadFeed(*feed)
	if err != nil {
		return fmt.Errorf("load feed: %w", err)
	}
	printStats(stdout, idx)
	return nil
}

func cmdServe(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	feed := fs.String("feed", defaultFeed, "directory containing the GTFS .txt files")
	addr := fs.String("addr", "localhost:8080", "address to listen on")
	if err := fs.Parse(args); err != nil {
		return err
	}

	idx, took, err := loadFeed(*feed)
	if err != nil {
		return fmt.Errorf("load feed: %w", err)
	}
	st := idx.Stats
	log.Printf("loaded %s in %s: %d routes, %d trips, %d stops, %d stop times, %d shapes",
		*feed, took.Round(time.Millisecond), st.Routes, st.Trips, st.Stops, st.StopTimes, st.Shapes)
	for r, n := range st.Skipped {
		log.Printf("skipped %d %s", n, r)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(idx, web.FS),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("listening on http://%s", *addr)
	return srv.ListenAndServe()
}
