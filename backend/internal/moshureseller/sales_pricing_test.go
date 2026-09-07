package moshureseller

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
	"time"
)

type pricingAdmin struct {
	service.AdminService
	t    *testing.T
	rate float64
}

func (a *pricingAdmin) CreateGroup(_ context.Context, input *service.CreateGroupInput) (*service.Group, error) {
	require.Equal(a.t, a.rate, input.RateMultiplier)
	require.True(a.t, input.AllowZeroRateMultiplier)
	return &service.Group{ID: 10, RateMultiplier: input.RateMultiplier}, nil
}
func (a *pricingAdmin) UpdateGroup(_ context.Context, _ int64, input *service.UpdateGroupInput) (*service.Group, error) {
	require.NotNil(a.t, input.RateMultiplier)
	require.Equal(a.t, a.rate, *input.RateMultiplier)
	require.True(a.t, input.AllowZeroRateMultiplier)
	return &service.Group{ID: 10, RateMultiplier: *input.RateMultiplier}, nil
}
func (a *pricingAdmin) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	require.Equal(a.t, 5.0, *input.RateMultiplier) // Upstream cost is untouched.
	return &service.Account{ID: 20}, nil
}

func pricingRows(selected bool, rate float64, groupID, accountID any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "remote", "code", "name", "platform", "group", "authorized", "selected", "cost", "sales", "version", "models", "capabilities", "credential", "local_group", "local_account", "effective"}).AddRow(3, 7, "gpt", "GPT", "openai", 9, true, selected, 5.0, rate, 1, []byte(`[]`), []byte(`{}`), "test-key", groupID, accountID, time.Now())
}

func TestConfigureProductAllowsPricesBelowCostAndZero(t *testing.T) {
	for _, rate := range []float64{0, 0.01, 1, 5, 9} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRows(false, 1, nil, nil))
		mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow("https://main.example", "instance", 1, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "etag", 1, nil, nil, nil))
		mock.ExpectExec("UPDATE moshu_products SET selected=TRUE").WithArgs(int64(3), rate, int64(10), int64(20)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRows(true, rate, 10, 20))
		sut := NewService(db, testEncryptor{}, &pricingAdmin{t: t, rate: rate})
		product, err := sut.ConfigureProduct(context.Background(), 3, true, "My price", rate)
		require.NoError(t, err)
		require.Equal(t, rate, *product.SalesRateMultiplier)
		_, err = sut.ensureGroup(context.Background(), *product, "My price", rate)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}

func TestConfigureProductRejectsInvalidNumbersWithoutWrites(t *testing.T) {
	for _, rate := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRows(false, 1, nil, nil))
		_, err = NewService(db, testEncryptor{}, nil).ConfigureProduct(context.Background(), 3, true, "Invalid", rate)
		require.ErrorIs(t, err, ErrInvalidInput)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}
