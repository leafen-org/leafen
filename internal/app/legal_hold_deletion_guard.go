package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An organization's legal hold keeps content from permanent deletion. The
// organization module decides that (PermanentDeleteHold); without it nothing
// is held. The core's deletion paths ask through
// assertNoActiveLegalHoldForPermanentDelete and answer a hold alike.

var (
	errLegalHoldUnavailable             = &legalHoldFailure{code: errorCodeLegalHoldUnavailable}
	errLegalHoldPreventsPermanentDelete = &legalHoldFailure{code: errorCodeLegalHoldPreventsPermanentDelete}
)

type legalHoldFailure struct {
	code string
}

func newLegalHoldFailure(code string) *legalHoldFailure {
	return &legalHoldFailure{code: code}
}

func (failure *legalHoldFailure) Error() string {
	return failure.code
}

func legalHoldFailureCode(err error) string {
	failure := &legalHoldFailure{}
	if errors.As(err, &failure) {
		return failure.code
	}
	return ""
}

type legalHoldDeletionTarget struct {
	WorkspaceID      string
	PageIDs          []string
	CustodianUserIDs []string
	EntireWorkspace  bool
}

// assertNoActiveLegalHoldForPermanentDelete fails with
// errLegalHoldPreventsPermanentDelete when an active legal hold covers the
// target.
func assertNoActiveLegalHoldForPermanentDelete(
	app core.App,
	organizationID string,
	target legalHoldDeletionTarget,
) error {
	if check := organizationPolicy.PermanentDeleteHold; check != nil {
		return check(app, organizationID, target)
	}
	// Without the module a hold can't be checked: while any is active,
	// nothing is permanently deleted.
	if held, err := legalHoldsKeptWithoutModule(app); err != nil {
		return errors.Join(errLegalHoldUnavailable, err)
	} else if held {
		return errLegalHoldUnenforced
	}
	return nil
}

func loadLegalHoldWorkspaceOrganization(app core.App, workspaceID string) (string, error) {
	var organizationID string
	if err := app.DB().NewQuery(`
		SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&organizationID); err != nil {
		return "", errors.Join(errLegalHoldUnavailable, err)
	}
	if organizationID == "" {
		return "", errLegalHoldUnavailable
	}
	return organizationID, nil
}
