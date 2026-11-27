package app

import (
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// loadWorkspaceSearchContentStates reports which candidates are in the
// content state asked for. A page is archived when it or a page above it
// is; a database item when it is or the page holding its database is. A
// verified page is a live wiki page whose verification has not expired.
func loadWorkspaceSearchContentStates(
	app core.App, content string, facts map[string]workspaceSearchCandidateFacts,
) (map[string]bool, error) {
	states := make(map[string]bool, len(facts))
	if content == workspaceSearchContentAll {
		for id := range facts {
			states[id] = true
		}
		return states, nil
	}
	anchors := make([]string, 0, len(facts))
	for _, fact := range facts {
		anchors = append(anchors, fact.Anchor)
	}
	effects, err := loadPageArchiveEffects(app, anchors)
	if err != nil {
		return nil, err
	}
	var verified map[string]wikiPageMetadata
	if content == workspaceSearchContentVerified {
		pageIDs := []string{}
		for id, fact := range facts {
			if fact.Anchor == id {
				pageIDs = append(pageIDs, id)
			}
		}
		if verified, err = loadWikiPageMetadataSet(app, pageIDs); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	for id, fact := range facts {
		_, anchorArchived := effects[fact.Anchor]
		archived := anchorArchived || strings.TrimSpace(fact.Archived) != ""
		switch content {
		case workspaceSearchContentArchived:
			states[id] = archived
		case workspaceSearchContentVerified:
			metadata, isWikiPage := verified[id]
			states[id] = !archived && isWikiPage && wikiVerificationCurrent(metadata, now)
		default:
			states[id] = !archived
		}
	}
	return states, nil
}

// wikiVerificationCurrent reports whether a wiki page is verified and its
// verification has not run out.
func wikiVerificationCurrent(metadata wikiPageMetadata, now time.Time) bool {
	if metadata.VerifiedAt == "" {
		return false
	}
	if metadata.VerificationExpiresAt == "" {
		return true
	}
	expires, err := time.Parse(time.RFC3339Nano, metadata.VerificationExpiresAt)
	return err == nil && expires.After(now)
}
