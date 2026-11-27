package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The pages of a copy known before they are made: those its tree's reading
// came to, and those whose IDs were chosen ahead — by a copied body's blocks
// (planBlockCopyPages), or by the copy's top as it went first
// (planPageDuplicationTopMentions). A link to one shows it coming until it
// is made.

// pageDuplicationChosenPages are the copies' IDs already chosen for these
// pages, which the tree's reading gives them when it comes to them
// (discoverPageDuplicationWindow): a copied body's blocks named them, or the
// copy's top did as it went first.
func pageDuplicationChosenPages(app core.App, jobID string, pageIDs []string) (map[string]string, error) {
	planned, err := pageDuplicationPlannedPages(app, jobID, pageIDs)
	if err != nil {
		return nil, err
	}
	named, err := pageDuplicationReferenceMappings(app, jobID, "page", pageIDs)
	if err != nil {
		return nil, err
	}
	for sourceID, targetID := range named {
		planned[sourceID] = targetID
	}
	return planned, nil
}

// pageDuplicationTargetToCome says a page the reader can't yet open is a copy
// still being made in the workspace of the page that links to it: one of its
// pages read, or one whose ID its top chose as it went first
// (planPageDuplicationTopMentions).
func pageDuplicationTargetToCome(app core.App, pageID, workspaceID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_duplication_jobs}} [[job]]
		WHERE [[job]].[[workspace_id]]={:workspaceId} AND [[job]].[[phase]] IN ('discovering','copying','publishing')
			AND (EXISTS (SELECT 1 FROM {{leafen_page_duplication_items}} [[item]]
					WHERE [[item]].[[job_id]]=[[job]].[[job_id]] AND [[item]].[[target_page_id]]={:pageId})
				OR EXISTS (SELECT 1 FROM {{leafen_page_duplication_identity_map}} [[chosen]]
					WHERE [[chosen]].[[job_id]]=[[job]].[[job_id]] AND [[chosen]].[[kind]]='page' AND [[chosen]].[[target_id]]={:pageId}))
	`).Bind(dbx.Params{"pageId": pageID, "workspaceId": workspaceID}).Row(&count)
	return count > 0, err
}

// pageBlockMention is the closer's kind for a sub-page or link-to-page block
// naming a page (content_graph_reader_mentions.go), which a page of the
// reader's copy still being made opens to, as a page mention doesn't.
const pageBlockMention = "pageBlock"

// ownCopyToComeCloser is how what a person reads on a page they opened
// stands to them, as memoMentionCloser's: a page or database they may not
// open is closed, or missing where nothing answers to it. A page of a copy
// they are still making is missing in a mention, as Notion's "찾을 수 없음"
// by a page's mark (seen 2026-11-15 three times, two tabs: a copy's top
// mentioning a page two levels down read so until that page was made, then
// its title), and opens as a sub-page block, which shows as the copy is made
// (2026-11-11: the copy's sub-pages read closed stayed hidden after it was
// done). It keeps what it learned for the one answer it serves; told the
// targets it will be asked of, it works them out together
// (mention_closer_learning.go).
func ownCopyToComeCloser(app core.App, principalID string) searchMentionCloser {
	return ownCopyToComeLearningCloser(app, principalID).closes
}

// ownCopyToComeReach is how a target stands to the person reading, given
// whether they may open the page it names and whether that page is of a copy
// they are still making.
func ownCopyToComeReach(app core.App, mention, pageID string, opens, toCome bool) mentionReach {
	switch {
	case opens:
		return mentionOpens
	case toCome && mention == pageBlockMention:
		return mentionOpens
	case toCome:
		return mentionMissing
	case pageRecordExists(app, pageID):
		return mentionClosed
	default:
		return mentionMissing
	}
}

// pagesOfOwnCopyToCome are, of the pages, those of a copy the person is still
// making: they are theirs to open once made, as the copy's top already is.
func pagesOfOwnCopyToCome(app core.App, principalID string, pageIDs []string) (map[string]bool, error) {
	toCome := map[string]bool{}
	if principalID == "" || len(pageIDs) == 0 {
		return toCome, nil
	}
	params, placeholders := pageDuplicationIDParams("page", pageIDs)
	params["actor"] = principalID
	found := []string{}
	// Its pages read, and those whose IDs its top chose as it went first (planPageDuplicationTopMentions).
	err := app.DB().NewQuery(`
		SELECT [[item]].[[target_page_id]] FROM {{leafen_page_duplication_jobs}} [[job]]
		JOIN {{leafen_page_duplication_items}} [[item]] ON [[item]].[[job_id]]=[[job]].[[job_id]]
			AND [[item]].[[target_page_id]] IN (` + joinSQL(placeholders) + `)
		WHERE [[job]].[[actor_id]]={:actor} AND [[job]].[[phase]] IN ('discovering','copying','publishing')
		UNION
		SELECT [[chosen]].[[target_id]] FROM {{leafen_page_duplication_jobs}} [[job]]
		JOIN {{leafen_page_duplication_identity_map}} [[chosen]] ON [[chosen]].[[job_id]]=[[job]].[[job_id]]
			AND [[chosen]].[[kind]]='page' AND [[chosen]].[[target_id]] IN (` + joinSQL(placeholders) + `)
		WHERE [[job]].[[actor_id]]={:actor} AND [[job]].[[phase]] IN ('discovering','copying','publishing')
	`).Bind(params).Column(&found)
	for _, pageID := range found {
		toCome[pageID] = true
	}
	return toCome, err
}
