package service

import (
	"testing"
	"time"
)

func TestPelicanShowcaseGroupVisibility(t *testing.T) {
	public := &Group{ID: 1, IsExclusive: false}
	exclusive := &Group{ID: 2, IsExclusive: true}

	if !showcaseGroupVisible(public, nil, false) {
		t.Fatal("public group should be visible without an allow-list")
	}
	if showcaseGroupVisible(exclusive, nil, false) {
		t.Fatal("exclusive group should be hidden without an allow-list")
	}
	if !showcaseGroupVisible(exclusive, map[int64]struct{}{2: {}}, true) {
		t.Fatal("allowed exclusive group should be visible")
	}
	if showcaseGroupVisible(exclusive, map[int64]struct{}{1: {}}, true) {
		t.Fatal("unallowed exclusive group should be hidden")
	}
}

func TestNormalizeScheduledTestKind(t *testing.T) {
	if got := normalizeScheduledTestKind(ScheduledTestKindPelican); got != ScheduledTestKindPelican {
		t.Fatalf("pelican kind normalized to %q", got)
	}
	if got := normalizeScheduledTestKind("unknown"); got != ScheduledTestKindConnection {
		t.Fatalf("unknown kind normalized to %q", got)
	}
}

func TestComputeNextRunUsesCronExpression(t *testing.T) {
	from := time.Date(2026, 10, 30, 12, 34, 0, 0, time.UTC)
	next, err := computeNextRun("*/5 * * * *", from)
	if err != nil {
		t.Fatalf("compute next run: %v", err)
	}
	want := time.Date(2026, 10, 30, 12, 35, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next run = %s, want %s", next, want)
	}
}
