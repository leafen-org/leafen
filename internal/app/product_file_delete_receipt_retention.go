package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errFileDeleteReceiptCleanupFailed = errors.New(errorCodeFileDeleteReceiptCleanupFailed)

type productFileDeleteReceiptRetentionResult struct {
	Deleted int
	HasMore bool
	NextDue int64
}

type productFileDeleteReceiptRetentionCandidate struct {
	ActorID     string `db:"actor_id"`
	RequestID   string `db:"request_id"`
	CompletedAt int64  `db:"completed_at"`
}

func retireProductFileDeleteReceipts(
	ctx context.Context,
	app core.App,
	now int64,
) (productFileDeleteReceiptRetentionResult, error) {
	result := productFileDeleteReceiptRetentionResult{}
	if err := ctx.Err(); err != nil {
		return result, errors.Join(errFileDeleteReceiptCleanupFailed, err)
	}
	if now <= 0 {
		return result, errFileDeleteReceiptCleanupFailed
	}
	retentionMillis := int64(fileDeleteReceiptReplayRetentionSeconds) * 1000
	cutoff := now - retentionMillis
	err := app.RunInTransaction(func(txApp core.App) error {
		candidates := []productFileDeleteReceiptRetentionCandidate{}
		limit := fileDeleteReceiptCleanupTurnLimit + 1
		if err := txApp.DB().NewQuery(`
			SELECT [[actor_id]], [[request_id]], [[completed_at]]
			FROM {{leafen_file_delete_receipts}}
			INDEXED BY [[idx_leafen_file_delete_receipts_retention]]
			WHERE [[status]] = {:status} AND [[completed_at]] <= {:cutoff}
			ORDER BY [[completed_at]], [[actor_id]], [[request_id]]
			LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"status": fileDeleteStatusCompleted,
			"cutoff": cutoff,
			"limit":  limit,
		}).All(&candidates); err != nil {
			return err
		}
		result.HasMore = len(candidates) > fileDeleteReceiptCleanupTurnLimit
		if result.HasMore {
			candidates = candidates[:fileDeleteReceiptCleanupTurnLimit]
		}
		if len(candidates) > 0 {
			params := dbx.Params{"status": fileDeleteStatusCompleted}
			identities := make([]string, 0, len(candidates))
			for index, candidate := range candidates {
				actorKey, requestKey := fmt.Sprintf("actor%d", index), fmt.Sprintf("request%d", index)
				completedKey := fmt.Sprintf("completed%d", index)
				params[actorKey], params[requestKey] = candidate.ActorID, candidate.RequestID
				params[completedKey] = candidate.CompletedAt
				identities = append(identities,
					"({:"+actorKey+"},{:"+requestKey+"},{:"+completedKey+"})",
				)
			}
			deleted, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_file_delete_receipts}}
				WHERE [[status]] = {:status}
					AND ([[actor_id]],[[request_id]],[[completed_at]]) IN (` +
				strings.Join(identities, ",") + `)
			`).WithContext(ctx).Bind(params).Execute()
			if err != nil {
				return err
			}
			count, err := deleted.RowsAffected()
			if err != nil {
				return err
			}
			if count != int64(len(candidates)) {
				return errors.New("selected file delete receipt changed during retirement")
			}
			result.Deleted = len(candidates)
		}
		if result.HasMore {
			result.NextDue = now
			return nil
		}
		var nextCompletedAt sql.NullInt64
		if err := txApp.DB().NewQuery(`
			SELECT MIN([[completed_at]]) FROM {{leafen_file_delete_receipts}}
			INDEXED BY [[idx_leafen_file_delete_receipts_retention]]
			WHERE [[status]] = {:status}
		`).WithContext(ctx).Bind(dbx.Params{
			"status": fileDeleteStatusCompleted,
		}).Row(&nextCompletedAt); err != nil {
			return err
		}
		if nextCompletedAt.Valid {
			result.NextDue = nextCompletedAt.Int64 + retentionMillis
		}
		return nil
	})
	if err != nil {
		return productFileDeleteReceiptRetentionResult{},
			errors.Join(errFileDeleteReceiptCleanupFailed, err)
	}
	return result, nil
}
