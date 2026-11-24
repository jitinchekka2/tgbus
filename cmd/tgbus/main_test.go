package main

import (
	"bytes"
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
		{name: "help", args: []string{"help"}, wantStdout: []string{"Usage:", "tgbus serve", "tgbus stats"}},
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
