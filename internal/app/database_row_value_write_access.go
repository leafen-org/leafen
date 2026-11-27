package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// What a person may change in a database's rows, asked once for the rows a
// change names and again for each row it moves along their dependencies
// (database_dependency_date_shift.go): a row's values only where its
// database page isn't locked for them (database_row_page_lock.go), its
// properties' access rules let them (database_property_access_writes.go),
// and its own sharing lets them edit it (database_row_unlinked_access.go).

// refuseRowValueWrites refuses the operations that change what the person
// may not change in database rows; enforceLock asks the database pages'
// locks too, where the writing surface keeps them.
func refuseRowValueWrites(app core.App, principalID string, enforceLock bool, operations []graphOperation) error {
	if enforceLock {
		if err := refuseLockedRowValueChanges(app, principalID, operations); err != nil {
			return err
		}
	}
	if principalID == "" {
		return nil
	}
	if err := refusePropertyAccessChanges(app, principalID, operations); err != nil {
		return err
	}
	return refuseUnlinkedRowChanges(app, principalID, operations)
}

// refusePersonRowValueWrites is a person's content change as what they may
// change in database rows lets it be made: refused where refuseRowValueWrites
// refuses it — but a relation pair's other side, which is not theirs to
// change and comes as Notion's does — and with People's own values kept as
// its rule keeps them (keepPeopleOwnValues), but for Leafen's own upkeep of
// People and an import put back.
func refusePersonRowValueWrites(app core.App, principalID string, hooks contentGraphMutationHooks, operations []graphOperation) ([]graphOperation, error) {
	if !hooks.MirrorsRelationPair {
		if err := refuseRowValueWrites(app, principalID, hooks.EnforcePageLock, operations); err != nil {
			return nil, err
		}
	}
	if hooks.LeafenOwn || hooks.WorkspaceImportReplaceRollback || principalID == "" {
		return operations, nil
	}
	return keepPeopleOwnValues(app, principalID, operations)
}

// isRowValueWriteRefusal is whether refuseRowValueWrites refused the change
// for what the person may do, rather than failing to read it.
func isRowValueWriteRefusal(err error) bool {
	var requestError *graphRequestError
	if !errors.As(err, &requestError) {
		return false
	}
	switch requestError.code {
	case errorCodePageLocked, errorCodeGraphPropertyAccessDenied, errorCodeGraphRowAccessDenied:
		return true
	}
	return false
}

// rowValueWritable says whether the person may make an operation's change
// to its row, by the refusals their own edit of it meets.
func rowValueWritable(app core.App, principalID string, enforceLock bool) func(graphOperation) (bool, error) {
	return func(operation graphOperation) (bool, error) {
		err := refuseRowValueWrites(app, principalID, enforceLock, []graphOperation{operation})
		if isRowValueWriteRefusal(err) {
			return false, nil
		}
		return err == nil, err
	}
}
