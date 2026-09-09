package moshureseller

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type costAdmin struct {
	service.AdminService
	t    *testing.T
	rate float64
	fail bool
}

func (a *costAdmin) GetAccount(context.Context, int64) (*service.Account, error) {
	return &service.Account{ID: 20}, nil
}

func (a *costAdmin) UpdateAccount(_ context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	if a.fail {
		return nil, errors.New("account temporarily unavailable")
	}
	require.Equal(a.t, int64(20), id)
	require.Nil(a.t, input.GroupIDs)
	require.Nil(a.t, input.Credentials)
	require.Nil(a.t, input.Concurrency)
	require.Empty(a.t, input.Status)
	a.rate = *input.RateMultiplier
	return &service.Account{ID: id}, nil
}

func TestSetProductCostPersistsOverrideAndUpdatesOnlyAccountCost(t *testing.T) {
	zero, custom := 0.0, 0.1234
	for _, rate := range []*float64{&zero, &custom, nil} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		admin := &costAdmin{t: t}
		mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRows(false, 1.7, nil, 20))
		mock.ExpectExec("UPDATE moshu_products SET cost_rate_override").WithArgs(int64(3), rate).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRowsWithCost(false, 1.7, nil, 20, rate))
		product, err := NewService(db, nil, admin).SetProductCost(context.Background(), 3, rate)
		require.NoError(t, err)
		require.Equal(t, 5.0, product.CostRateMultiplier) // Public rate is retained.
		require.Equal(t, rate, product.CostRateOverride)
		require.Equal(t, product.EffectiveCostRate(), admin.rate)
		require.Equal(t, 1.7, *product.SalesRateMultiplier)
		require.False(t, product.Selected)
		require.Nil(t, product.LocalGroupID)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}

func TestSetProductCostRejectsInvalidRatesBeforeAnyDatabaseAccess(t *testing.T) {
	for _, rate := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1), 1000000} {
		_, err := NewService(nil, nil, nil).SetProductCost(context.Background(), 3, &rate)
		require.ErrorIs(t, err, ErrInvalidInput)
	}
}

func TestSetProductCostFailureBoundaries(t *testing.T) {
	for _, accountFailure := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		rate := 0.4
		admin := &costAdmin{t: t, fail: accountFailure, rate: 5}
		mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRows(false, 1.7, nil, 20))
		write := mock.ExpectExec("UPDATE moshu_products SET cost_rate_override").WithArgs(int64(3), rate)
		if accountFailure {
			write.WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRowsWithCost(false, 1.7, nil, 20, rate))
		} else {
			write.WillReturnError(errors.New("database temporarily unavailable"))
		}
		_, err = NewService(db, nil, admin).SetProductCost(context.Background(), 3, &rate)
		require.Error(t, err)
		if accountFailure {
			require.ErrorContains(t, err, "cost setting saved; local account update pending")
		}
		require.Equal(t, 5.0, admin.rate)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}

func TestCatalogReconciliationPreservesLocalCostOverride(t *testing.T) {
	for _, rate := range []float64{0, 0.23} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		previous := 1.0
		admin := &catalogAdmin{t: t,
			account: service.Account{ID: 20, Platform: "openai", Type: "apikey", Status: "active", RateMultiplier: &previous,
				Credentials: map[string]any{"api_key": "keep", "custom": "preserved"},
				Extra:       map[string]any{"custom": "preserved", "openai_passthrough": true}},
		}
		sut := NewService(db, nil, admin)
		for i := 0; i < 2; i++ {
			mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRowsWithCost(true, 1.7, nil, 20, rate))
			require.NoError(t, sut.applyCatalogConfiguration(context.Background()))
		}
		require.Equal(t, rate, *admin.account.RateMultiplier)
		require.Equal(t, 1, admin.accountUpdates)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}

func TestNewGroupUsesManualCostWithoutChangingCustomerMultiplier(t *testing.T) {
	for _, cost := range []float64{0, 0.15} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRowsWithCost(false, 1.7, nil, nil, cost))
		mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow("https://main.example", "instance", 1, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "etag", 1, nil, nil, nil))
		mock.ExpectExec("UPDATE moshu_products SET selected=TRUE").WithArgs(int64(3), 1.7, int64(10), int64(20)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRowsWithCost(true, 1.7, 10, 20, cost))
		sut := NewService(db, testEncryptor{}, &pricingAdmin{t: t, rate: 1.7, capacity: 37, cost: &cost})
		product, err := sut.ConfigureProduct(context.Background(), 3, true, "Custom group", 1.7, 37)
		require.NoError(t, err)
		require.Equal(t, cost, product.EffectiveCostRate())
		require.Equal(t, 1.7, *product.SalesRateMultiplier)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}
