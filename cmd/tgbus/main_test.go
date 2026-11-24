package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErr    string
		wantStdout []string
	}{
		{name: "no command", args: nil, wantErr: "missing command"},
		{name: "unknown command", args: []string{"frobnicate"}, wantErr: `unknown command "frobnicate"`},
		{name: "help", args: []string{"help"}, wantStdout: []string{"Usage:", "tgbus build", "tgbus serve", "tgbus stats"}},
		{name: "missing feed dir", args: []string{"stats", "-feed", "../../testdata/nope"}, wantErr: "load feed"},
		{name: "bad flag", args: []string{"stats", "-nope"}, wantErr: "flag provided but not defined"},
		{
			name: "stats on fixture feed",
			args: []string{"stats", "-feed", "../../testdata/feed"},
			wantStdout: []string{
				"routes:     3", "stops:      4", "stop times: 10", "skipped 1 stops with bad id or coordinates",
			},
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
			} else if err != nil {
				t.Fatalf("unexpected error: %v (stderr %q)", err, stderr.String())
			}
			for _, want := range tt.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing %q; got:\n%s", want, stdout.String())
				}
			}
		})
	}
}

func TestBuildFeedLabel(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "defaults to the feed directory name", args: nil, want: `"feed":"feed"`},
		{name: "explicit name wins", args: []string{"-name", "TGSRTC 2026"}, want: `"feed":"TGSRTC 2026"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "site")
			args := append([]string{"build", "-feed", "../../testdata/feed", "-out", out}, tt.args...)
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatalf("build: %v", err)
			}
			meta, err := os.ReadFile(filepath.Join(out, "data", "meta.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(meta), tt.want) {
				t.Errorf("meta.json = %s, want containing %s", meta, tt.want)
			}
		})
	}
}

func TestBuildCommand(t *testing.T) {
	out := filepath.Join(t.TempDir(), "site")
	var stdout, stderr bytes.Buffer
	err := run([]string{"build", "-feed", "../../testdata/feed", "-out", out}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("build: %v (stderr %q)", err, stderr.String())
	}
	for _, want := range []string{"routes:     3", "wrote "} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q; got:\n%s", want, stdout.String())
		}
	}
	tests := []struct {
		name string
		path string
	}{
		{name: "page", path: "index.html"},
		{name: "logo and favicon", path: "logo.svg"},
		{name: "module", path: "js/app.js"},
		{name: "vendored map library", path: "vendor/maplibre-gl/maplibre-gl.js"},
		{name: "meta", path: "data/meta.json"},
		{name: "a route", path: "data/routes/R1.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(tt.path))); err != nil {
				t.Errorf("missing %s: %v", tt.path, err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(out, "embed.go")); err == nil {
		t.Error("Go source file embed.go must not be copied into the site")
	}
}

func TestSiteHandler(t *testing.T) {
	built := t.TempDir()
	if err := os.WriteFile(filepath.Join(built, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		dir     string
		wantErr string
		url     string
		want    string
	}{
		{name: "missing directory", dir: filepath.Join(built, "nope"), wantErr: "run `tgbus build"},
		{name: "directory without index", dir: t.TempDir(), wantErr: "has no index.html"},
		{name: "serves the page", dir: built, url: "/", want: "<h1>hi</h1>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := siteHandler(tt.dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", tt.url, nil))
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("status %d body %q, want 200 containing %q", rec.Code, rec.Body, tt.want)
			}
		})
	}
}
