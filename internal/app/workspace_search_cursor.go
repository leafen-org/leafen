package app

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// workspaceSearchScoreValid admits a result's rank: a tier for best matches
// (workspace_search_tiers.go), the last the pages found by their path holding
// some keywords, or a time for a sort by time.
func workspaceSearchScoreValid(score int64, bestMatch bool) bool {
	return !bestMatch || score >= workspaceSearchTierExact && score <= workspaceSearchTierSomePath
}

func decodeWorkspaceSearchCursor(raw, principalID, workspaceID, queryHash string, bestMatch bool) (workspaceSearchCursor, error) {
	cursor := workspaceSearchCursor{PrincipalID: principalID, WorkspaceID: workspaceID, QueryHash: queryHash}
	if strings.TrimSpace(raw) == "" {
		return cursor, nil
	}
	if err := decodeSearchCursor(raw, &cursor); err != nil || cursor.PrincipalID != principalID || cursor.WorkspaceID != workspaceID || cursor.QueryHash != queryHash || cursor.PageID == "" || !workspaceSearchScoreValid(cursor.Score, bestMatch) || cursor.Row < 0 {
		return workspaceSearchCursor{}, permissionDenied()
	}
	return cursor, nil
}

func decodeWorkspaceSearchIndexCursor(
	raw, principalID, workspaceID, queryHash string, bestMatch bool,
) (workspaceSearchIndexCursor, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return workspaceSearchIndexCursor{}, false, nil
	}
	cursor := workspaceSearchIndexCursor{}
	if err := decodeSearchCursor(raw, &cursor); err != nil ||
		cursor.PrincipalID != principalID || cursor.WorkspaceID != workspaceID ||
		cursor.QueryHash != queryHash || cursor.Revision < 0 ||
		len(cursor.CandidatePageIDs) != len(cursor.CandidateScores) ||
		len(cursor.CandidatePageIDs) != len(cursor.CandidateRows) ||
		len(cursor.CandidatePageIDs) > cursor.Limit+1 ||
		cursor.Limit < 1 || cursor.Limit > maxWorkspaceSearchResults ||
		!workspaceSearchScoreValid(cursor.AfterScore, bestMatch) ||
		!workspaceSearchScoreValid(cursor.CheckedScore, bestMatch) ||
		cursor.AfterRow < 0 || cursor.CheckedRow < 0 ||
		(cursor.AfterPageID == "" && (cursor.AfterScore != 0 || cursor.AfterRow != 0)) ||
		(cursor.CheckedPageID == "" && (cursor.CheckedScore != 0 || cursor.CheckedRow != 0 || len(cursor.CandidatePageIDs) > 0)) {
		return workspaceSearchIndexCursor{}, false, permissionDenied()
	}
	seen := map[string]struct{}{}
	found := workspaceSearchCursorCandidates(cursor)
	for index, candidate := range found {
		if candidate.PageID == "" || candidate.Row < 0 || !workspaceSearchScoreValid(candidate.Score, bestMatch) {
			return workspaceSearchIndexCursor{}, false, permissionDenied()
		}
		if _, exists := seen[candidate.PageID]; exists {
			return workspaceSearchIndexCursor{}, false, permissionDenied()
		}
		if index > 0 && !workspaceSearchInOrder(found[index-1], candidate, bestMatch) {
			return workspaceSearchIndexCursor{}, false, permissionDenied()
		}
		seen[candidate.PageID] = struct{}{}
	}
	return cursor, true, nil
}

func decodeWorkspaceSearchRebuildCursor(raw, principalID, workspaceID string) (workspaceSearchRebuildCursor, error) {
	cursor := workspaceSearchRebuildCursor{PrincipalID: principalID, WorkspaceID: workspaceID}
	if strings.TrimSpace(raw) == "" {
		return cursor, nil
	}
	if err := decodeSearchCursor(raw, &cursor); err != nil || cursor.PrincipalID != principalID || cursor.WorkspaceID != workspaceID || cursor.PageID == "" {
		return workspaceSearchRebuildCursor{}, permissionDenied()
	}
	return cursor, nil
}

func decodeSearchCursor(raw string, target any) error {
	if len(raw) > maxWorkspaceSearchCursorBytes {
		return permissionDenied()
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > maxWorkspaceSearchCursorBytes {
		return permissionDenied()
	}
	return json.Unmarshal(data, target)
}

func encodeWorkspaceSearchCursor(cursor workspaceSearchCursor) (string, error) {
	return encodeSearchCursor(cursor)
}

func encodeWorkspaceSearchIndexCursor(cursor workspaceSearchIndexCursor) (string, error) {
	return encodeSearchCursor(cursor)
}

func encodeSearchCursor(cursor any) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	if len(data) > maxWorkspaceSearchCursorBytes || len(encoded) > maxWorkspaceSearchCursorBytes {
		return "", permissionDenied()
	}
	return encoded, nil
}
