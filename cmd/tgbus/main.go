// Command tgbus turns a GTFS bus feed into a static, explorable website: it
// can print statistics, build the site, and serve a built site for preview.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jitinchekka2/tgbus/index"
	"github.com/jitinchekka2/tgbus/site"
	"github.com/jitinchekka2/tgbus/web"
)

const defaultFeed = "Telangana_opendata_gtfs_TGSRTC_25_September_2026"

const usage = `tgbus: turn a GTFS bus feed into a static, explorable website

Usage:
  tgbus build [-feed dir] [-out dir] [-name label]
                                       generate the static site (default out: dist)
  tgbus serve [-dir dir] [-addr h:p]   serve a built site locally for preview
  tgbus plan  [-feed dir] -from lat,lon -to lat,lon [-at HH:MM]
                                       plan a bus journey (try -from-stop / -to-stop with names)
  tgbus stats [-feed dir]              print feed statistics and skipped rows

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
	case "build":
		return cmdBuild(args[1:], stdout, stderr)
	case "serve":
		return cmdServe(args[1:], stderr)
	case "plan":
		return cmdPlan(args[1:], stdout, stderr)
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

func cmdBuild(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	feed := fs.String("feed", defaultFeed, "directory containing the GTFS .txt files")
	out := fs.String("out", "dist", "output directory for the static site")
	name := fs.String("name", "", "feed label shown on the site (default: the feed directory's name)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	label := *name
	if label == "" {
		label = filepath.Base(filepath.Clean(*feed))
	}
	idx, took, err := loadFeed(*feed)
	if err != nil {
		return fmt.Errorf("load feed: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "loaded %s in %s\n", *feed, took.Round(time.Millisecond))
	printStats(stdout, idx)

	rep, err := site.Build(idx, site.Options{Out: *out, Assets: web.FS, FeedName: label})
	if err != nil {
		return fmt.Errorf("build site: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "wrote %d files (%.1f MB) to %s\n", rep.Files, float64(rep.Bytes)/1e6, *out)
	return nil
}

// siteHandler serves a built site, or explains how to build one.
func siteHandler(dir string) (http.Handler, error) {
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return nil, fmt.Errorf("%s has no index.html; run `tgbus build -out %s` first", dir, dir)
	}
	return http.FileServer(http.Dir(dir)), nil
}

func cmdServe(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "dist", "directory of a site built with `tgbus build`")
	addr := fs.String("addr", "localhost:8080", "address to listen on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	h, err := siteHandler(*dir)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("serving %s on http://%s", *dir, *addr)
	return srv.ListenAndServe()
}
