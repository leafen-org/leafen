package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func settleTeamspacePublication(
	ctx context.Context, app core.App, rooms workspaceRoomInvalidator, receiptID string,
) (bool, error) {
	job := struct {
		ID, WorkspaceID, Status string
	}{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[status]] FROM {{leafen_teamspace_publication_jobs}}
		WHERE [[receipt_id]]={:receiptId}
	`).WithContext(ctx).Bind(dbx.Params{"receiptId": receiptID}).One(&job)
	if err != nil || job.Status == teamspacePublicationDone {
		return job.Status == teamspacePublicationDone, err
	}
	if rooms == nil || rooms.InvalidateWorkspace(job.WorkspaceID) != nil {
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_teamspace_publication_jobs}} SET [[attempts]]=[[attempts]]+1,[[updated_at]]={:now}
			WHERE [[id]]={:id} AND [[status]]='pending'
		`).WithContext(ctx).Bind(dbx.Params{"now": time.Now().UTC().UnixMilli(), "id": job.ID}).Execute()
		return false, err
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_teamspace_publication_jobs}} SET [[status]]='completed',
				[[attempts]]=[[attempts]]+1,[[updated_at]]={:now}
			WHERE [[id]]={:id} AND [[status]]='pending'
		`).Bind(dbx.Params{"now": time.Now().UTC().UnixMilli(), "id": job.ID}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return errors.Join(err, fmt.Errorf("teamspace publication settlement conflict"))
		}
		receipt := storedTeamspaceReceipt{}
		if err = txApp.DB().NewQuery(`
			SELECT [[id]],[[workspace_id]],[[request_hash]],[[response_json]]
			FROM {{leafen_teamspace_mutation_receipts}} WHERE [[id]]={:id}
		`).Bind(dbx.Params{"id": receiptID}).One(&receipt); err != nil {
			return err
		}
		response := teamspaceMutationResponse{}
		if json.Unmarshal([]byte(receipt.ResponseJSON), &response) != nil {
			return errTeamspaceConflict
		}
		response.PublicationPending = false
		encoded, _ := json.Marshal(response)
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_teamspace_mutation_receipts}} SET [[response_json]]={:response} WHERE [[id]]={:id}
		`).Bind(dbx.Params{"response": string(encoded), "id": receiptID}).Execute()
		return err
	})
	return err == nil, err
}
