package main

import (
	"testing"
	"time"
)

func TestParseSinceFlag(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, got time.Time)
	}{
		{
			name:  "duration 24h",
			input: "24h",
			check: func(t *testing.T, got time.Time) {
				diff := time.Since(got)
				if diff < 23*time.Hour || diff > 25*time.Hour {
					t.Errorf("expected ~24h ago, got %v ago", diff)
				}
			},
		},
		{
			name:  "date 2026-03-01",
			input: "2026-03-01",
			check: func(t *testing.T, got time.Time) {
				want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
				if !got.Equal(want) {
					t.Errorf("got %v, want %v", got, want)
				}
			},
		},
		{
			name:  "duration 1h30m",
			input: "1h30m",
			check: func(t *testing.T, got time.Time) {
				diff := time.Since(got)
				if diff < 89*time.Minute || diff > 91*time.Minute {
					t.Errorf("expected ~90m ago, got %v ago", diff)
				}
			},
		},
		{
			name:    "invalid input",
			input:   "not-a-date",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSinceFlag(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseSinceFlag(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestIsBypassEvent(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want bool
	}{
		{name: "forced land", kind: "land_forced", want: true},
		{name: "hooks skipped", kind: "hooks_skipped", want: true},
		{name: "normal event", kind: "workspace.created", want: false},
		{name: "contains forced uppercase", kind: "LAND_FORCED", want: true},
		{name: "empty kind", kind: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBypassEvent(tt.kind)
			if got != tt.want {
				t.Errorf("isBypassEvent(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}
