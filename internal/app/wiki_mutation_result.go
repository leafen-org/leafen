package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/pocketbase/core"
)

const wikiMutationResultSchemaVersion = 1

func captureWikiMutationResult(
	app core.App,
	job storedWikiMutation,
	target *core.Record,
) (*wikiMutationResult, string, error) {
	if !app.IsTransactional() || target.Id != job.TargetPageID ||
		target.GetString("workspace") != job.WorkspaceID {
		return nil, "", errWikiConflict
	}
	// A wiki has no owners of its own, as in Notion; its pages' owners are
	// its database's Owner values.
	result := &wikiMutationResult{
		SchemaVersion: wikiMutationResultSchemaVersion,
		WorkspaceID:   job.WorkspaceID, PageID: job.TargetPageID,
		WikiRootID: job.RootPageID,
		IsWiki:     job.TargetPageID == job.RootPageID && target.GetBool("is_wiki"),
		Revision:   target.GetInt("wiki_revision"), OwnerIDs: []string{},
		VerifiedAt:            target.GetString("verified_at"),
		VerifiedBy:            target.GetString("verified_by"),
		VerificationExpiresAt: target.GetString("verification_expires_at"),
	}
	if !validWikiMutationResult(job, result) {
		return nil, "", errWikiConflict
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, "", err
	}
	return result, string(payload), nil
}

func decodeWikiMutationResult(job storedWikiMutation) (*wikiMutationResult, error) {
	if job.Status != durableJobStatusCompleted {
		return nil, nil
	}
	result := &wikiMutationResult{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), result); err != nil {
		return nil, errWikiConflict
	}
	if !validWikiMutationResult(job, result) {
		return nil, errWikiConflict
	}
	if result.OwnerIDs == nil {
		result.OwnerIDs = []string{}
	}
	return result, nil
}

func validWikiMutationResult(job storedWikiMutation, result *wikiMutationResult) bool {
	if result == nil || result.SchemaVersion != wikiMutationResultSchemaVersion ||
		result.WorkspaceID != job.WorkspaceID || result.PageID != job.TargetPageID ||
		result.WikiRootID != job.RootPageID || result.Revision != job.TargetRevision ||
		len(result.OwnerIDs) > maxWikiOwnersPerPage ||
		(result.VerifiedAt == "") != (result.VerifiedBy == "") ||
		(result.VerifiedAt == "" && result.VerificationExpiresAt != "") {
		return false
	}
	// A wiki whose pages are its database's pages has no owners of its own;
	// its pages' owners are their Owner values.
	expectedWiki := job.TargetPageID == job.RootPageID && job.Operation != wikiOperationUndo
	if result.IsWiki != expectedWiki ||
		(job.Operation == wikiOperationUndo && len(result.OwnerIDs) != 0) {
		return false
	}
	for index, ownerID := range result.OwnerIDs {
		if ownerID == "" || (index > 0 && result.OwnerIDs[index-1] >= ownerID) {
			return false
		}
	}
	return sort.StringsAreSorted(result.OwnerIDs)
}
