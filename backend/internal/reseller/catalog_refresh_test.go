package reseller

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type productSnapshotAdmin struct {
	service.AdminService
	models []string
	err    error
}

func (a *productSnapshotAdmin) GetGroupModelsListCandidates(context.Context, int64, string) ([]string, error) {
	return append([]string(nil), a.models...), a.err
}

func TestRefreshProductSnapshotsUsesEffectiveBillingRate(t *testing.T) {
	// SQL COALESCE, not truthiness, preserves an exclusive zero multiplier.
	for _, rate := range []float64{0, 0.15, 1.5} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectQuery(`SELECT rp.id,g.id,g.platform,COALESCE\(ugr.rate_multiplier,g.rate_multiplier\).*JOIN reseller_tenants rt.*LEFT JOIN user_group_rate_multipliers ugr ON ugr.user_id=rt.user_id AND ugr.group_id=g.id`).
			WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id", "group_id", "platform", "rate", "models"}).AddRow(7, 9, "openai", rate, []byte(`{}`)))
		mock.ExpectExec(`UPDATE reseller_products rp SET.*COALESCE\(ugr.rate_multiplier,g.rate_multiplier\)=\$4::numeric`).
			WithArgs(int64(7), int64(9), "openai", rate, sqlmock.AnyArg(), []byte(`{}`)).WillReturnResult(sqlmock.NewResult(0, 1))
		require.NoError(t, NewService(db, nil, nil).refreshProductSnapshots(context.Background(), 3))
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}

func TestRefreshProductSnapshotsPropagatesSourceFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("SELECT rp.id,g.id,g.platform").WillReturnError(errors.New("source unavailable"))
	require.ErrorContains(t, NewService(db, nil, nil).refreshProductSnapshots(context.Background(), 1), "source unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResolveProductModelsSnapshotUsesGroupCandidatesWhenAllowlistIsDisabled(t *testing.T) {
	svc := NewService(nil, nil, nil)
	svc.pricingAdmin = &productSnapshotAdmin{models: []string{"kimi-k2.6", "kimi-k2.5", "kimi-k2.6"}}

	models, err := svc.resolveProductModelsSnapshot(
		context.Background(), 7, service.PlatformKimi,
		[]byte(`{"enabled":false,"models":[]}`),
	)

	require.NoError(t, err)
	require.Equal(t, []string{"kimi-k2.6", "kimi-k2.5"}, models)
}

func TestResolveProductModelsSnapshotKeepsExplicitMainGroupAllowlist(t *testing.T) {
	svc := NewService(nil, nil, nil)
	svc.pricingAdmin = &productSnapshotAdmin{models: []string{"kimi-k2.6", "kimi-k2.5"}}

	models, err := svc.resolveProductModelsSnapshot(
		context.Background(), 7, service.PlatformKimi,
		[]byte(`{"enabled":true,"models":["kimi-k2.5"]}`),
	)

	require.NoError(t, err)
	require.Equal(t, []string{"kimi-k2.5"}, models)
}

func TestResolveProductModelsSnapshotExpandsAllowlistWithoutAdvertisingWildcards(t *testing.T) {
	for _, tt := range []struct {
		name       string
		raw        string
		candidates []string
		want       []string
	}{
		{
			name:       "exact custom model absent from defaults",
			raw:        `{"enabled":true,"models":["private-model"]}`,
			candidates: []string{"kimi-k2.6"},
			want:       []string{"private-model"},
		},
		{
			name:       "wildcard expands defaults and concrete account mapping aliases",
			raw:        `{"enabled":true,"models":["kimi-*","private-model","kimi-k2.6"]}`,
			candidates: []string{"kimi-k2.6", "kimi-custom-alias", "deepseek-flash", "kimi-*", "*"},
			want:       []string{"kimi-k2.6", "kimi-custom-alias", "private-model"},
		},
		{
			name:       "unmatched wildcard yields empty catalogue",
			raw:        `{"enabled":true,"models":["unavailable-*"]}`,
			candidates: []string{"kimi-k2.6", "unavailable-*"},
			want:       []string{},
		},
		{
			name:       "global wildcard excludes mapping patterns",
			raw:        `{"enabled":true,"models":["*"]}`,
			candidates: []string{"kimi-k2.6", "kimi-custom-alias", "kimi-*", "*"},
			want:       []string{"kimi-k2.6", "kimi-custom-alias"},
		},
		{
			name:       "disabled allowlist still excludes mapping patterns",
			raw:        `{"enabled":false,"models":["private-model"]}`,
			candidates: []string{"kimi-k2.6", "kimi-*", "*"},
			want:       []string{"kimi-k2.6"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(nil, nil, nil)
			svc.pricingAdmin = &productSnapshotAdmin{models: tt.candidates}
			models, err := svc.resolveProductModelsSnapshot(context.Background(), 7, service.PlatformKimi, []byte(tt.raw))
			require.NoError(t, err)
			require.Equal(t, tt.want, models)
		})
	}
}

func TestResolveProductModelsSnapshotWildcardWithoutCandidateProviderKeepsOnlyExplicitIDs(t *testing.T) {
	svc := NewService(nil, nil, nil)
	models, err := svc.resolveProductModelsSnapshot(context.Background(), 7, service.PlatformKimi,
		[]byte(`{"enabled":true,"models":["kimi-*","private-model"]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"private-model"}, models)
}

func TestResolveProductModelsSnapshotPropagatesWildcardCandidateFailure(t *testing.T) {
	svc := NewService(nil, nil, nil)
	svc.pricingAdmin = &productSnapshotAdmin{err: errors.New("candidate source unavailable")}
	_, err := svc.resolveProductModelsSnapshot(context.Background(), 7, service.PlatformKimi,
		[]byte(`{"enabled":true,"models":["kimi-*"]}`))
	require.ErrorContains(t, err, "candidate source unavailable")
}
