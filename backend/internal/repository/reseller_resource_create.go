package repository

import (
	"context"
	"database/sql"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// The product row is the durable idempotency record. A crash after creation,
// a lost commit response, or a later BindGroups failure cannot orphan a second
// resource on retry. Row locking also serializes creators across processes.
func lockResellerResources(ctx context.Context, client *dbent.Client, productID int64) (sql.NullInt64, sql.NullInt64, error) {
	var groupID, accountID sql.NullInt64
	rows, err := client.QueryContext(ctx, `SELECT local_group_id,local_account_id FROM moshu_products WHERE id=$1 AND authorized=TRUE FOR UPDATE`, productID)
	if err != nil {
		return groupID, accountID, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return groupID, accountID, err
		}
		return groupID, accountID, fmt.Errorf("reseller product is missing or revoked")
	}
	err = rows.Scan(&groupID, &accountID)
	return groupID, accountID, err
}

func (r *groupRepository) createResellerGroup(ctx context.Context, productID int64, input *service.Group) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	groupID, _, err := lockResellerResources(ctx, client, productID)
	if err != nil {
		return err
	}
	if groupID.Valid {
		existing, err := newGroupRepositoryWithSQL(client, client).GetByID(ctx, groupID.Int64)
		if err != nil {
			return err
		}
		*input = *existing
		return tx.Commit()
	}
	if err := createGroupRecord(ctx, client, input); err != nil {
		return err
	}
	if _, err = client.ExecContext(ctx, `UPDATE moshu_products SET local_group_id=$2,updated_at=NOW() WHERE id=$1`, productID, input.ID); err != nil {
		return err
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventGroupChanged, nil, &input.ID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *accountRepository) createResellerAccount(ctx context.Context, productID int64, input *service.Account) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	_, accountID, err := lockResellerResources(ctx, client, productID)
	if err != nil {
		return err
	}
	if accountID.Valid {
		existing, err := newAccountRepositoryWithSQL(client, client, r.schedulerCache).GetByID(ctx, accountID.Int64)
		if err != nil {
			return err
		}
		*input = *existing
		return tx.Commit()
	}
	if err := createAccountRecord(ctx, client, input); err != nil {
		return err
	}
	if _, err = client.ExecContext(ctx, `UPDATE moshu_products SET local_account_id=$2,updated_at=NOW() WHERE id=$1`, productID, input.ID); err != nil {
		return err
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &input.ID, nil, buildSchedulerGroupPayload(input.GroupIDs)); err != nil {
		return err
	}
	return tx.Commit()
}
