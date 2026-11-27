package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// The caller holds the page's public-mutation lock before entering any
// transaction. This lets an access decision commit its share and decision
// together with the same mutation validation, receipt and durable effects.
func executePublicMutationTransaction(
	ctx context.Context, app core.App, actorID, pageID string, input publicMutationInput,
) (publicMutationResponse, bool, error) {
	normalizePublicMutationInput(&input)
	if actorID == "" || pageID == "" {
		return publicMutationResponse{}, false, errPublicMutationInvalid
	}
	if err := validatePublicMutationInput(input); err != nil {
		return publicMutationResponse{}, false, err
	}
	requestHash, err := publicMutationRequestHash(pageID, input)
	if err != nil {
		return publicMutationResponse{}, false, err
	}
	turn := publicMutationTurn{}
	err = app.RunInTransaction(func(txApp core.App) error {
		currentAuthorization, err := authorizePublicMutationFor(txApp, actorID, pageID, input)
		if err != nil {
			return err
		}
		existing, err := loadPublicMutationOperationByRequest(txApp, actorID, input.RequestID)
		if err == nil {
			if existing.WorkspaceID != currentAuthorization.Workspace.WorkspaceID ||
				existing.PageID != pageID || existing.RequestHash != requestHash {
				return errPublicMutationRequestReused
			}
			if err := json.Unmarshal([]byte(existing.ResponseJSON), &turn.Response); err != nil {
				return failedBecause(errPublicMutationFailed, err)
			}
			turn.Response.Operation = projectPublicMutationOperation(existing)
			turn.Response.Replayed = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := enforcePublicMutationPolicies(txApp, currentAuthorization, input); err != nil {
			return err
		}
		if err := enforcePersonPageShare(txApp, currentAuthorization, input); err != nil {
			return err
		}
		if !approvedGuestInvite(ctx) {
			if err := enforceGuestInvitePolicy(txApp, currentAuthorization, input); err != nil {
				return err
			}
			if err := enforceRestrictedMemberShare(txApp, currentAuthorization, input); err != nil {
				return err
			}
		}

		stored, err := loadStoredPublicMutationState(txApp, pageID)
		if errors.Is(err, sql.ErrNoRows) {
			stored = storedPublicMutationState{
				WorkspaceID: currentAuthorization.Workspace.WorkspaceID, PageID: pageID,
			}
		} else if err != nil {
			return err
		}
		expectedRevision := input.ExpectedRevision
		if stored.Revision != expectedRevision {
			return errPublicMutationRevisionConflict
		}
		now := time.Now().UTC()
		applied, err := applyPublicMutation(
			ctx, txApp, currentAuthorization, stored, input, expectedRevision+1, now,
		)
		if err != nil {
			return err
		}
		if err := storePublicMutationState(txApp, stored.Revision, applied.State); err != nil {
			return err
		}
		// Who may only take their own share away reads no one they could share with.
		state, err := readPublicMutationState(ctx, txApp, currentAuthorization, "", "", true,
			currentAuthorization.AccessRole >= pageAccessRoleFullAccess)
		if err != nil {
			return err
		}
		operation := storedPublicMutationOperation{
			ID: security.RandomString(15), WorkspaceID: applied.State.WorkspaceID,
			PageID: pageID, ActorID: actorID, RequestID: input.RequestID,
			RequestHash: requestHash, Action: input.Action, Revision: applied.State.Revision,
			Status: publicMutationOperationPending, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
		}
		turn.Response = publicMutationResponse{
			State: state, Operation: projectPublicMutationOperation(operation),
			Replayed: false, Token: applied.Token,
		}
		encoded, err := json.Marshal(turn.Response)
		if err != nil {
			return err
		}
		operation.ResponseJSON = string(encoded)
		if err := insertPublicMutationOperation(txApp, operation); err != nil {
			return err
		}
		effectInput := input
		if applied.Changed != nil {
			effectInput.PrincipalType, effectInput.PrincipalID = applied.Changed.PrincipalType, applied.Changed.PrincipalID
		}
		if err := admitPublicMutationEffects(
			txApp, currentAuthorization, operation, effectInput, applied.State, now,
		); err != nil {
			return err
		}
		turn.Fresh = true
		return nil
	})
	if err != nil {
		return publicMutationResponse{}, false, err
	}
	return turn.Response, turn.Fresh, nil
}
