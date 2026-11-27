package app

import "github.com/pocketbase/pocketbase/core"

// A reading that leaves the trash out (readablePageIDs: a person's search;
// an agent's reads as its Markdown does, mention_agent_trash_closer.go)
// reads a mention of a database in the trash closed, as it
// reads a mention of a page there: a database under a page there through
// the page holding it (memoMentionCloser), and one in the trash itself while
// that page is out of it — its block sent there alone or deleted with it, or
// the mentioned data source sent there — by the database's own trash, as
// its mention's mark on a person's screen says it is in the trash
// (readDatabaseMentionPreview, data_source_mention_preview.go).

// trashedDatabasesClosed is the closer of a reading that leaves the trash
// out, given the closer judging what a mention names by its page.
func trashedDatabasesClosed(app core.App, closes searchMentionCloser) searchMentionCloser {
	trashed := map[string]bool{}
	return func(mention, targetID string) mentionReach {
		reach := closes(mention, targetID)
		if reach != mentionOpens || mention != "database" {
			return reach
		}
		inTrash, known := trashed[targetID]
		if !known {
			inTrash = mentionedDatabaseInTrash(app, targetID)
			trashed[targetID] = inTrash
		}
		if inTrash {
			return mentionClosed
		}
		return reach
	}
}

// mentionedDatabaseInTrash says whether the database a mention names — by
// its block's ID, or a data source by its address — is in the trash itself;
// one it fails to read counts as out of it.
func mentionedDatabaseInTrash(app core.App, databaseID string) bool {
	containerID := databaseID
	sourceID, sourceContainerID, _, found, err := mentionedDataSource(app, databaseID)
	if err != nil {
		return false
	}
	if found {
		if inTrash, err := databaseSourceIsTrashed(app, sourceID); err == nil && inTrash {
			return true
		}
		containerID = sourceContainerID
	}
	trashed, err := trashedDatabaseContainerIDs(app, []string{containerID})
	return err == nil && trashed[containerID]
}
