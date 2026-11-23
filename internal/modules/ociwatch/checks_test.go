package ociwatch

import (
	"math"
	"testing"
	"time"
)

func TestExistsRule(t *testing.T) {
	cases := map[string]bool{
		"RUNNING":     true,
		"PROVISIONING": true,
		"STARTING":    true,
		"STOPPED":     true, // stopped ≠ gone; it still exists and is reported
		"TERMINATING": false,
		"TERMINATED":  false,
		"":            true,
	}
	for state, want := range cases {
		if got := exists(state); got != want {
			t.Errorf("exists(%q) = %v, want %v", state, got, want)
		}
	}
}

func TestParseMemAvailPct(t *testing.T) {
	content := "MemTotal:       24005960 kB\nMemFree:         8011608 kB\nMemAvailable:   17005960 kB\nBuffers:          372536 kB\n"
	got, err := parseMemAvailPct(content)
	if err != nil {
		t.Fatal(err)
	}
	want := 17005960.0 / 24005960.0 * 100
	if math.Abs(got-want) > 0.001 {
		t.Fatalf("parseMemAvailPct = %v, want %v", got, want)
	}
	if _, err := parseMemAvailPct("MemTotal: 100 kB\n"); err == nil {
		t.Fatal("missing MemAvailable must error")
	}
}

func TestParseLoad5(t *testing.T) {
	got, err := parseLoad5("0.12 0.34 0.56 1/500 12345")
	if err != nil || math.Abs(got-0.34) > 1e-9 {
		t.Fatalf("parseLoad5 = %v, %v; want 0.34", got, err)
	}
	if _, err := parseLoad5("garbage"); err == nil {
		t.Fatal("short loadavg must error")
	}
}

func TestParseUptime(t *testing.T) {
	got, err := parseUptime("102123.45 987654.32")
	if err != nil {
		t.Fatal(err)
	}
	if diff := got - 102123*time.Second; diff < -time.Second || diff > time.Second {
		t.Fatalf("parseUptime = %v, want ~102123s", got)
	}
}

func TestFormatUp(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{40 * time.Second, "40s"},
		{12 * time.Minute, "12m"},
		{3*time.Hour + 20*time.Minute, "3h 20m"},
		{28 * time.Hour, "1d 4h"},
	}
	for _, c := range cases {
		if got := formatUp(c.d); got != c.want {
			t.Errorf("formatUp(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestFormatAgo(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{2 * time.Minute, "2 min"},
		{3*time.Hour + 5*time.Minute, "3 h 5 min"},
		{26 * time.Hour, "1 d 2 h"},
	}
	for _, c := range cases {
		if got := formatAgo(c.d); got != c.want {
			t.Errorf("formatAgo(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate must not touch short strings, got %q", got)
	}
	long := string(make([]rune, 250))
	if got := truncate(long, 200); len([]rune(got)) != 201 { // 200 + ellipsis
		t.Fatalf("truncate kept %d runes", len([]rune(got)))
	}
}
