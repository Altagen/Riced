package schedule_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Altagen/Riced/internal/schedule"
)

func TestParseDayRange(t *testing.T) {
	cases := []struct {
		in      string
		wantDay string
		wantNgt string
		wantErr bool
	}{
		{"07:00-19:00", "07:00", "19:00", false},
		{"00:00-23:59", "00:00", "23:59", false},
		{"19:00-07:00", "19:00", "07:00", false}, // overnight is fine
		{"7:00-19:00", "", "", true},             // single-digit hour rejected
		{"07:00 to 19:00", "", "", true},         // wrong separator
		{"24:00-07:00", "", "", true},            // hour out of range
		{"07:60-08:00", "", "", true},            // minute out of range
		{"", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			d, n, err := schedule.ParseDayRange(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got day=%q night=%q", d, n)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d != tc.wantDay || n != tc.wantNgt {
				t.Errorf("got (%q, %q), want (%q, %q)", d, n, tc.wantDay, tc.wantNgt)
			}
		})
	}
}

// TestList_OnlyRicedTimers verifies that List scans the user systemd
// directory for `riced-mode-day-*.timer` files specifically, ignoring
// unrelated user timers and the matching .service files.
func TestList_OnlyRicedTimers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	dir := schedule.UnitDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	must := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("riced-mode-day-s4-red.timer")
	must("riced-mode-night-s4-red.timer") // not in the prefix List scans -> ignored
	must("riced-mode-day-s4-red.service") // service, not a timer -> ignored
	must("riced-mode-day-mono.timer")
	must("some-other-user.timer") // unrelated user timer -> ignored

	got, err := schedule.List(home)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mono", "s4-red"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
