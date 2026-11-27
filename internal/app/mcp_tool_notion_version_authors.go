package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// get-version-changes names each of its authors as the page's history in
// the app names who made a version, whoever reads it
// (pageHistoryActorProfiles): by their account's name now, and a moved
// workspace's person without an account here by the name the move brought —
// however they came among the authors, by a version, a discussion started on
// what changed (mcp_version_discussion_edits.go) or a write that left a
// block as it was (mcp_version_touch_edits.go). (2026-11-24: one who had
// only commented in the range was named "", the names read from the
// versions' own makers alone.) One not known, the system's own writes among
// them, keeps "".

// mcpVersionAuthors are the people who made changes, in the order the
// changes first name them, each named.
func mcpVersionAuthors(app core.App, changes []notionMCPVersionChange) ([]notionMCPVersionAuthor, error) {
	authors, seen := []notionMCPVersionAuthor{}, map[string]bool{}
	accounts := map[string]string{}
	for _, change := range changes {
		for _, contributor := range change.Contributors {
			if contributor.Author == "" || seen[contributor.Author] {
				continue
			}
			seen[contributor.Author] = true
			authors = append(authors, notionMCPVersionAuthor{URL: contributor.Author})
			// An account by its Notion-compatible ID, a moved person by the ID kept.
			account, err := resolveNotionCompatibleUserID(notionPersonReferenceID(contributor.Author))
			if err != nil {
				account = notionPersonReferenceID(contributor.Author)
			}
			if account != "" {
				accounts[account] = contributor.Author
			}
		}
	}
	ids := make([]string, 0, len(accounts))
	for account := range accounts {
		ids = append(ids, account)
	}
	names := map[string]string{}
	// As many at a time as the app's lists of profiles name.
	for start := 0; start < len(ids); start += commentNotificationInboxMaxSize {
		profiles, err := pageHistoryActorProfiles(app, ids[start:min(start+commentNotificationInboxMaxSize, len(ids))])
		if err != nil {
			return nil, failedBecause(errMCPToolFailed, err)
		}
		for _, profile := range profiles {
			names[accounts[profile.UserID]] = strings.TrimSpace(profile.DisplayName)
		}
	}
	for index := range authors {
		authors[index].Name = names[authors[index].URL]
	}
	return authors, nil
}
