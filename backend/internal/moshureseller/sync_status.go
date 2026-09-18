package moshureseller

import (
	"context"
	"database/sql"
	"time"
)

func (s *Service) loadSyncDomains(ctx context.Context, connection *Connection) {
	if connection == nil {
		return
	}
	var authSuccess, authErrorAt, catalogSuccess, catalogErrorAt, pricingSuccess, pricingErrorAt, settlementSuccess, settlementErrorAt sql.NullTime
	var authError, catalogError, pricingError, settlementError sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT auth_last_success_at,auth_last_error_at,auth_last_error,
		       catalog_last_success_at,catalog_last_error_at,catalog_last_error,
		       pricing_last_success_at,pricing_last_error_at,pricing_last_error,
		       settlement_last_success_at,settlement_last_error_at,settlement_last_error
		FROM moshu_reseller_connections WHERE id=1`).Scan(
		&authSuccess, &authErrorAt, &authError,
		&catalogSuccess, &catalogErrorAt, &catalogError,
		&pricingSuccess, &pricingErrorAt, &pricingError,
		&settlementSuccess, &settlementErrorAt, &settlementError)
	if err != nil {
		return
	}
	connection.AuthSync = syncDomain(authSuccess, authErrorAt, authError)
	connection.CatalogSync = syncDomain(catalogSuccess, catalogErrorAt, catalogError)
	connection.PricingSync = syncDomain(pricingSuccess, pricingErrorAt, pricingError)
	connection.SettlementSync = syncDomain(settlementSuccess, settlementErrorAt, settlementError)
}

func syncDomain(success, errorAt sql.NullTime, message sql.NullString) SyncDomain {
	result := SyncDomain{LastError: message.String}
	if success.Valid {
		value := success.Time
		result.LastSuccessAt = &value
	}
	if errorAt.Valid {
		value := errorAt.Time
		result.LastErrorAt = &value
	}
	return result
}

func (s *Service) recordSyncDomainSuccess(ctx context.Context, domain string) {
	column := ""
	switch domain {
	case "auth":
		column = "auth"
	case "catalog":
		column = "catalog"
	case "pricing":
		column = "pricing"
	case "settlement":
		column = "settlement"
	default:
		return
	}
	// Sync callers use bounded contexts for the remote request.  If the remote
	// call completes just as that deadline is reached, using the same context
	// here silently drops the success marker and leaves the UI showing a stale
	// error.  Status bookkeeping is local and best-effort, so detach it from the
	// request cancellation while retaining a short database timeout.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(writeCtx, `UPDATE moshu_reseller_connections SET `+column+`_last_success_at=NOW(),`+column+`_last_error_at=NULL,`+column+`_last_error=NULL,updated_at=NOW() WHERE id=1`)
}

func (s *Service) recordSyncDomainFailure(ctx context.Context, domain string, cause error) {
	if cause == nil {
		return
	}
	column := ""
	switch domain {
	case "auth":
		column = "auth"
	case "catalog":
		column = "catalog"
	case "pricing":
		column = "pricing"
	case "settlement":
		column = "settlement"
	default:
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(writeCtx, `UPDATE moshu_reseller_connections SET `+column+`_last_error_at=NOW(),`+column+`_last_error=$1,updated_at=NOW() WHERE id=1`, truncate(cause.Error(), 1000))
}
