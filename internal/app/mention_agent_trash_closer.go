package app

import "github.com/pocketbase/pocketbase/core"

// An agent reads a mention's words — a title's, a cell's, a comment's, what
// its queries filter, search and sort by — as the page Markdown it reads beside
// them names the mention, Notion's "v2" Markdown and MCP
// (notionMentionTargetInTrash; seen 2026-11-25 twice through Notion's API):
// a page or database its person may open, in the trash too, and its token
// reaches, by its name; but a page in the trash itself, and a database there
// or under a page there, which that Markdown writes by its address alone, in
// the words an unnamed mention reads by ("Untitled"). A page under a page in
// the trash, a database page whose database is there among them, keeps its
// name, as there (2026-11-25: a fetch's title read such a mention "Untitled"
// while its Markdown named it). A person's search leaves the trash out
// instead (readablePageIDs, trashedDatabasesClosed).

// mcpAgentOpens is, of the pages, those an agent's reading may name: its
// person may open them, in the trash too (openablePageIDs), and its token
// reaches them.
func mcpAgentOpens(app core.App, authority mcpAuthority, pageIDs []string) (map[string]bool, error) {
	open, err := openablePageIDs(app, authority.PrincipalID, pageIDs)
	if err != nil {
		return open, err
	}
	for pageID := range open {
		if !mcpAuthorityReachesPageID(app, authority, pageID) {
			delete(open, pageID)
		}
	}
	return open, nil
}

// agentTrashClosed is the closer of an agent's reading, given the closer
// judging what a mention names by what the agent may open (mcpAgentOpens).
func agentTrashClosed(app core.App, closes searchMentionCloser) searchMentionCloser {
	trashed := map[string]bool{}
	return func(mention, targetID string) mentionReach {
		reach := closes(mention, targetID)
		if reach != mentionOpens || (mention != "page" && mention != "database") {
			return reach
		}
		key := mention + ":" + targetID
		inTrash, known := trashed[key]
		if !known {
			inTrash = agentMentionInTrash(app, mention, targetID)
			trashed[key] = inTrash
		}
		if inTrash {
			return mentionClosed
		}
		return reach
	}
}

// agentMentionInTrash is whether a page or database mention's target is in
// the trash as an agent's Markdown tells it (by its address alone): a page
// in the trash itself; a database there itself — its block, its data source
// or itself deleted — or under a page there, by the one rule of what the
// trash holds (pageInTrashPath). One it fails to read counts as out of it.
func agentMentionInTrash(app core.App, mention, targetID string) bool {
	if mention == "page" {
		place, err := notionPageTrashPlace(app, targetID)
		return err == nil && place == notionInTrashItself
	}
	if mentionedDatabaseInTrash(app, targetID) {
		return true
	}
	holder := mcpDatabaseHolderPageID(app, targetID)
	if holder == "" {
		return false
	}
	inTrash, err := pageInTrashPath(app, holder)
	return err == nil && inTrash
}
