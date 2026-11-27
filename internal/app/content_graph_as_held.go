package app

import "github.com/pocketbase/pocketbase/core"

// assertContentGraphFences holds a change to the body revision and the page
// cursor it was read at. A change kept on a device and sent as its page goes
// — a database the page starts as — had no time to read them first: sent as
// the page is held (asHeld), it is held to neither, as one read just before
// would be; the empty lines it takes away are still checked under the room's
// lock (keepDatabaseStartWriting).
func assertContentGraphFences(app core.App, ownerID, pageID string, page *core.Record, input graphMutationInput) error {
	if input.AsHeld {
		return nil
	}
	if err := assertContentGraphRevision(app, ownerID, pageID, input.BaseGraphRevision); err != nil {
		return err
	}
	return assertContentGraphPageCursor(page, input.BasePageCursor)
}
