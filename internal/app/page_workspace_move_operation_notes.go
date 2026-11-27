package app

// What the operations list says beside a page's two ways into another
// workspace on this server, each pointing to the other, so an agent chooses
// as the screen's card lets a person (useMoveToWorkspace.ts): a move keeps
// the page's IDs and leaves nothing behind (page_workspace_move.go); a
// duplicate leaves the page where it is and makes a copy there, as Notion's
// move to another workspace does. Both say what becomes of a linked view of a
// database on the other side: it stays and waits for its source
// (page_duplication_unlinked_views.go).

func init() {
	for id, note := range map[string]string{
		"POST /pages/{pageId}/workspace-move": "Moves the page, the pages under it and their databases to another workspace on this server: " +
			"their IDs, addresses, comments, history and favorites stay, and nothing stays behind. Relations to databases left behind, " +
			"the old workspace's shares and links, and others' favorites there are cut. Ask with dryRun: true first for the plan " +
			"(what moves and what will be cut) and show it to the person; peopleLosingAccessAtLeast true means at least that many lose access, " +
			"the count having stopped at the people it asks about. A linked view whose database ends up in the other workspace — on the moved " +
			"pages (across.linkedViewsOut) or left behind showing a moved database (across.linkedViewsIn) — stays, its views kept, and waits " +
			"for a data source there, showing that database again once both are in one workspace. To leave the page where " +
			"it is and make a copy in the other workspace instead, as Notion does, use POST /pages/{pageId}/duplicate with destinationWorkspaceId.",
		"POST /pages/{pageId}/duplicate": "With destinationWorkspaceId, a copy at new addresses in that workspace; the page stays where it is, " +
			"as Notion's move to another workspace does. To move the page itself there on this server instead, keeping its IDs and " +
			"addresses, use POST /pages/{pageId}/workspace-move. The copy takes the databases on the pages it copies; a linked view of a " +
			"database it doesn't take stays on the copy, its views kept, and waits there for a data source, as Notion's copy leaves it.",
	} {
		nativeOperationNotes[nativeOperationShape(id)] = note
	}
}
