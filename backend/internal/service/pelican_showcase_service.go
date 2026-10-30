package service

import (
	"context"
	"errors"
	"sort"
	"time"
)

// PelicanShowcaseResult is the read-only result shown on the user-facing
// group comparison page. It deliberately reuses scheduled account tests so
// the showcase does not introduce another request or billing path.
type PelicanShowcaseResult struct {
	ID int64 `json:"id"`
	// AccountName is retained internally for sorting/diagnostics, but must not
	// be exposed by the user-facing showcase. The page is a group comparison.
	AccountName    string    `json:"-"`
	ModelID        string    `json:"model_id"`
	ThinkingEffort string    `json:"thinking_effort,omitempty"`
	Status         string    `json:"status"`
	ResponseText   string    `json:"response_text,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	LatencyMs      int64     `json:"latency_ms"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
}

// PelicanShowcaseGroup is one visible group and its latest comparison data.
// Latest is the most recent successful result; LatestFailure is retained as a
// diagnostic note and never replaces an already available successful card.
type PelicanShowcaseGroup struct {
	ID            int64                    `json:"id"`
	Name          string                   `json:"name"`
	Platform      string                   `json:"platform"`
	Results       []*PelicanShowcaseResult `json:"results"`
	Latest        *PelicanShowcaseResult   `json:"latest,omitempty"`
	LatestFailure *PelicanShowcaseResult   `json:"latest_failure,omitempty"`
	Status        string                   `json:"status"`
	LastTestAt    *time.Time               `json:"last_test_at,omitempty"`
	SuccessCount  int                      `json:"success_count"`
	FailureCount  int                      `json:"failure_count"`
}

// PelicanShowcaseService assembles a read-only view from existing groups,
// accounts, scheduled plans and test results. Keeping this logic separate
// makes later upstream merges independent of the gateway and billing paths.
type PelicanShowcaseService struct {
	groupRepo   GroupRepository
	accountRepo AccountRepository
	planService *ScheduledTestService
}

func NewPelicanShowcaseService(
	groupRepo GroupRepository,
	accountRepo AccountRepository,
	planService *ScheduledTestService,
) *PelicanShowcaseService {
	return &PelicanShowcaseService{groupRepo: groupRepo, accountRepo: accountRepo, planService: planService}
}

// ListLatest returns active, visible-by-ID groups. A nil allowed map means
// the caller has not supplied a visibility restriction; an empty non-nil map
// means the authenticated user has no explicitly allowed exclusive groups.
func (s *PelicanShowcaseService) ListLatest(ctx context.Context, allowed map[int64]struct{}, restrictPublic bool) ([]PelicanShowcaseGroup, error) {
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	if bulk, err := s.listLatestBulk(ctx, groups, allowed, restrictPublic); err == nil {
		return bulk, nil
	} else if !errors.Is(err, ErrPelicanShowcaseReaderUnavailable) {
		return nil, err
	}
	out := make([]PelicanShowcaseGroup, 0, len(groups))
	for i := range groups {
		g := &groups[i]
		if !showcaseGroupVisible(g, allowed, restrictPublic) {
			continue
		}
		row := PelicanShowcaseGroup{ID: g.ID, Name: g.Name, Platform: g.Platform, Results: make([]*PelicanShowcaseResult, 0, 8), Status: "unknown"}
		seenPlans := make(map[int64]struct{})
		accounts, err := s.accountRepo.ListByGroup(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		for i := range accounts {
			account := &accounts[i]
			// Historical test results remain useful even when the account is
			// temporarily paused or rate-limited; do not hide the group's last
			// known result just because its current scheduler state changed.
			plans, err := s.planService.ListPlansByAccount(ctx, account.ID)
			if err != nil {
				return nil, err
			}
			for _, plan := range plans {
				if plan.TestKind != ScheduledTestKindPelican || !plan.Enabled {
					continue
				}
				if plan.GroupID == nil || *plan.GroupID != g.ID {
					continue
				}
				if _, seen := seenPlans[plan.ID]; seen {
					continue
				}
				seenPlans[plan.ID] = struct{}{}
				results, err := s.planService.ListResults(ctx, plan.ID, 8)
				if err != nil {
					return nil, err
				}
				for _, result := range results {
					candidate := &PelicanShowcaseResult{
						ID: result.ID, AccountName: account.Name, ModelID: plan.ModelID,
						// Existing scheduled tests use the provider's default reasoning
						// setting; keep the field explicit for the showcase and future
						// Pelican plans that may persist a selected effort.
						ThinkingEffort: "default",
						Status:         result.Status, ResponseText: result.ResponseText,
						ErrorMessage: result.ErrorMessage, LatencyMs: result.LatencyMs,
						StartedAt: result.StartedAt, FinishedAt: result.FinishedAt,
					}
					row.Results = append(row.Results, candidate)
				}
			}
		}
		// A group is shown only when an enabled, group-scoped Pelican plan
		// exists. This keeps unrelated visible groups out of the showcase while
		// still allowing a newly configured group with no result to show its
		// waiting state.
		if len(seenPlans) == 0 {
			continue
		}
		sort.SliceStable(row.Results, func(i, j int) bool {
			return row.Results[i].FinishedAt.After(row.Results[j].FinishedAt)
		})
		if len(row.Results) > 12 {
			row.Results = row.Results[:12]
		}
		for _, result := range row.Results {
			if row.LastTestAt == nil || result.FinishedAt.After(*row.LastTestAt) {
				finishedAt := result.FinishedAt
				row.LastTestAt = &finishedAt
			}
			if result.Status == "success" {
				row.SuccessCount++
				if row.Latest == nil {
					row.Latest = result
				}
			} else {
				row.FailureCount++
				if row.LatestFailure == nil {
					row.LatestFailure = result
				}
			}
		}
		if len(row.Results) > 0 {
			if time.Since(row.Results[0].FinishedAt) > 24*time.Hour {
				row.Status = "stale"
			} else if row.Results[0].Status == "success" {
				row.Status = "healthy"
			} else {
				row.Status = "degraded"
			}
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *PelicanShowcaseService) listLatestBulk(ctx context.Context, groups []Group, allowed map[int64]struct{}, restrictPublic bool) ([]PelicanShowcaseGroup, error) {
	visible := make([]Group, 0, len(groups))
	groupIDs := make([]int64, 0, len(groups))
	for _, group := range groups {
		if !showcaseGroupVisible(&group, allowed, restrictPublic) {
			continue
		}
		visible = append(visible, group)
		groupIDs = append(groupIDs, group.ID)
	}
	records, err := s.planService.ListLatestPelicanByGroups(ctx, groupIDs, 12)
	if err != nil {
		return nil, err
	}
	byGroup := make(map[int64][]PelicanShowcaseRecord, len(groupIDs))
	for _, record := range records {
		byGroup[record.GroupID] = append(byGroup[record.GroupID], record)
	}
	configured := make(map[int64]struct{}, len(groupIDs))
	hasConfiguredReader := false
	configuredIDs, configuredErr := s.planService.ListConfiguredPelicanGroups(ctx, groupIDs)
	if configuredErr == nil {
		hasConfiguredReader = true
		for _, groupID := range configuredIDs {
			configured[groupID] = struct{}{}
		}
	} else if !errors.Is(configuredErr, ErrPelicanShowcaseReaderUnavailable) {
		return nil, configuredErr
	}

	out := make([]PelicanShowcaseGroup, 0, len(visible))
	for _, group := range visible {
		if hasConfiguredReader {
			if _, ok := configured[group.ID]; !ok {
				continue
			}
		} else if len(byGroup[group.ID]) == 0 {
			// Compatibility repositories can expose results but not plan
			// metadata; do not render groups that have no visible result.
			continue
		}
		row := PelicanShowcaseGroup{ID: group.ID, Name: group.Name, Platform: group.Platform, Results: make([]*PelicanShowcaseResult, 0, 12), Status: "unknown"}
		for _, record := range byGroup[group.ID] {
			result := record.Result
			row.Results = append(row.Results, &PelicanShowcaseResult{
				ID: result.ID, AccountName: record.AccountName, ModelID: record.ModelID,
				ThinkingEffort: "default", Status: result.Status,
				ResponseText: result.ResponseText, ErrorMessage: result.ErrorMessage,
				LatencyMs: result.LatencyMs, StartedAt: result.StartedAt, FinishedAt: result.FinishedAt,
			})
		}
		finalizePelicanShowcaseGroup(&row)
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func finalizePelicanShowcaseGroup(row *PelicanShowcaseGroup) {
	for _, result := range row.Results {
		if row.LastTestAt == nil || result.FinishedAt.After(*row.LastTestAt) {
			finishedAt := result.FinishedAt
			row.LastTestAt = &finishedAt
		}
		if result.Status == "success" {
			row.SuccessCount++
			if row.Latest == nil {
				row.Latest = result
			}
		} else {
			row.FailureCount++
			if row.LatestFailure == nil {
				row.LatestFailure = result
			}
		}
	}
	if len(row.Results) == 0 {
		return
	}
	if time.Since(row.Results[0].FinishedAt) > 24*time.Hour {
		row.Status = "stale"
	} else if row.Results[0].Status == "success" {
		row.Status = "healthy"
	} else {
		row.Status = "degraded"
	}
}

func showcaseGroupVisible(group *Group, allowed map[int64]struct{}, restrictPublic bool) bool {
	if allowed == nil {
		return !group.IsExclusive
	}
	if group.IsExclusive || restrictPublic {
		_, ok := allowed[group.ID]
		return ok
	}
	return true
}
