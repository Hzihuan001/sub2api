package service

import (
	"encoding/json"
	"strings"
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

func TestScheduledTestFailureBackoffIsBounded(t *testing.T) {
	if got := scheduledTestFailureBackoff(1); got != time.Minute {
		t.Fatalf("first failure backoff = %s", got)
	}
	if got := scheduledTestFailureBackoff(3); got != 4*time.Minute {
		t.Fatalf("third failure backoff = %s", got)
	}
	if got := scheduledTestFailureBackoff(99); got != 8*time.Minute {
		t.Fatalf("bounded failure backoff = %s", got)
	}
}

func TestFinalizePelicanShowcaseKeepsOnlySuccessfulCards(t *testing.T) {
	now := time.Now()
	row := PelicanShowcaseGroup{
		ID:   17,
		Name: "public group",
		Results: []*PelicanShowcaseResult{
			{
				ID: 2, AccountName: "private-account", ModelID: "gpt-6.1-sol",
				Status: "failed", ErrorMessage: "private upstream error",
				FinishedAt: now, StartedAt: now.Add(-time.Second),
			},
			{
				ID: 1, AccountName: "private-account", ModelID: "gpt-6.1-sol",
				Status: "success", ResponseText: "completed work",
				FinishedAt: now.Add(-time.Minute), StartedAt: now.Add(-time.Minute - time.Second),
			},
		},
	}

	finalizePelicanShowcaseGroup(&row)
	if len(row.Results) != 1 || row.Results[0].Status != "success" {
		t.Fatalf("public result cards = %#v, want only the successful card", row.Results)
	}
	if row.Latest == nil || row.Latest.ID != 1 {
		t.Fatalf("latest success = %#v, want result 1", row.Latest)
	}
	if row.FailureCount != 1 || row.Status != "degraded" {
		t.Fatalf("group status/count = %q/%d, want degraded/1", row.Status, row.FailureCount)
	}

	body, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal showcase response: %v", err)
	}
	jsonText := string(body)
	for _, privateValue := range []string{"private-account", "private upstream error", "latest_failure", "error_message"} {
		if strings.Contains(jsonText, privateValue) {
			t.Errorf("public showcase JSON leaked %q: %s", privateValue, jsonText)
		}
	}
	if !strings.Contains(jsonText, `"response_text":"completed work"`) {
		t.Errorf("public showcase JSON omitted successful work: %s", jsonText)
	}
}

func TestFinalizePelicanShowcaseKeepsOlderSuccessOutsideHealthWindow(t *testing.T) {
	now := time.Now()
	results := make([]*PelicanShowcaseResult, 0, 51)
	for i := 0; i < 50; i++ {
		finishedAt := now.Add(-time.Duration(i) * time.Minute)
		results = append(results, &PelicanShowcaseResult{
			ID:         int64(100 + i),
			ModelID:    "gpt-6.1-sol",
			Status:     "failed",
			FinishedAt: finishedAt,
			StartedAt:  finishedAt.Add(-time.Second),
		})
	}
	olderSuccess := now.Add(-51 * time.Minute)
	results = append(results, &PelicanShowcaseResult{
		ID:           1,
		ModelID:      "gpt-6.1-sol",
		Status:       "success",
		ResponseText: "older successful work",
		FinishedAt:   olderSuccess,
		StartedAt:    olderSuccess.Add(-time.Second),
	})

	row := PelicanShowcaseGroup{Results: results}
	finalizePelicanShowcaseGroup(&row)

	if len(row.Results) != 1 || row.Results[0].ID != 1 {
		t.Fatalf("older successful card was dropped: %#v", row.Results)
	}
	if row.SuccessCount != 0 || row.FailureCount != 50 || row.Status != "degraded" {
		t.Fatalf("health window = %d successes/%d failures/%q, want 0/50/degraded", row.SuccessCount, row.FailureCount, row.Status)
	}
}
