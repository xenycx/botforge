package schedule

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestParseAndNext(t *testing.T) {
	utc := time.UTC
	start := time.Date(2026, 9, 30, 10, 7, 30, 0, utc) // Wednesday
	cases := []struct {
		spec string
		want []string
	}{
		{"*/15 * * * *", []string{"2026-09-30 10:15", "2026-09-30 10:30", "2026-09-30 10:45"}},
		{"0 3 * * *", []string{"2026-10-01 03:00", "2026-10-02 03:00"}},
		{"30 9 * * mon-fri", []string{"2026-10-01 09:30", "2026-10-02 09:30", "2026-10-05 09:30"}},
		{"0 0 1 * *", []string{"2026-10-01 00:00", "2026-11-01 00:00"}},
		{"@weekly", []string{"2026-10-04 00:00", "2026-10-11 00:00"}},
		{"0 12 13 * 5", []string{"2026-10-02 12:00", "2026-10-09 12:00", "2026-10-13 12:00"}}, // day 13 OR Friday
		{"0 0 31 2 *", nil}, // never happens
	}
	for _, c := range cases {
		s, err := Parse(c.spec)
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		got := s.NextN(start, utc, len(c.want))
		if len(c.want) == 0 && len(got) != 0 {
			t.Fatalf("%s: expected no runs, got %v", c.spec, got)
		}
		for i, w := range c.want {
			if i >= len(got) || got[i].Format("2006-01-02 15:04") != w {
				t.Fatalf("%s: run %d = %v, want %s (all %v)", c.spec, i, got, w, got)
			}
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "5-1 * * * *", "*/0 * * * *", "0 0 0 * *", "x * * * *"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestDaylightSavingTransitions(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	// 2026-03-08: 02:00-02:59 does not exist in New York.
	s, _ := Parse("30 2 * * *")
	next := s.Next(time.Date(2026, 3, 7, 12, 0, 0, 0, ny), ny)
	if got := next.In(ny).Format("2006-01-02 15:04"); got != "2026-03-09 02:30" {
		t.Fatalf("skipped wall time: next = %s, want the following day", got)
	}
	// 2026-11-01: 01:00-01:59 happens twice; run once.
	s, _ = Parse("30 1 * * *")
	first := s.Next(time.Date(2026, 10, 31, 12, 0, 0, 0, ny), ny)
	second := s.Next(first, ny)
	if first.In(ny).Format("2006-01-02 15:04 MST") != "2026-11-01 01:30 EDT" {
		t.Fatalf("first = %s", first.In(ny).Format("2006-01-02 15:04 MST"))
	}
	if second.In(ny).Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("the repeated hour ran twice: second = %s", second.In(ny))
	}
}

func TestImpossibleScheduleNeverRuns(t *testing.T) {
	s, _ := Parse("0 0 31 2 *")
	if next := s.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC); !next.IsZero() {
		t.Fatalf("February 31 ran at %v", next)
	}
}
