package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where create-comment puts a comment is read for that comment alone, not
// from the page's every comment, so a page takes an agent's comment however
// many it holds, as it takes a person's (2026-11-24: refused past 25,000):
// a reply's discussion from the comment it names up its parents, and a
// comment given to the page alone from the page's newest open discussions,
// a window at a time until its own is found.

// mcpOpenPageDiscussionWindow is how many of a target's latest first
// comments the first read for its open discussion takes; each next read
// takes twice as many (eachWindow).
const mcpOpenPageDiscussionWindow = 50

// mcpCommentReadAuthorizer is how the comment tools read a target's
// comments: as the agent may read them now.
func mcpCommentReadAuthorizer(authority mcpAuthority, target nativeCommentTarget) func(core.App) (commentAuthorization, error) {
	return func(txApp core.App) (commentAuthorization, error) {
		return authorizeMCPCommentTargetCurrent(txApp, authority, target, permissionWorkspaceRead)
	}
}

// mcpCommentReplyRoot is the first comment of the discussion the comment id
// names is in, on the target; false for one not there.
func mcpCommentReplyRoot(app core.App, authority mcpAuthority, target nativeCommentTarget, id string) (commentRecord, bool, error) {
	root, found := commentRecord{}, false
	err := readCommentTargetSnapshot(app, target, mcpCommentReadAuthorizer(authority, target), func(txApp core.App, workspaceID string) error {
		seen := map[string]bool{}
		for at := id; ; {
			if seen[at] {
				return errMCPToolFailed
			}
			seen[at] = true
			records, err := readCommentTargetWindow(txApp, workspaceID, target, `AND [[id]]={:commentId}`, dbx.Params{"commentId": at}, 1)
			if err != nil {
				return err
			}
			switch {
			case len(records) == 0 && at == id:
				return nil
			case len(records) == 0:
				// A reply whose parent is gone.
				return errMCPToolFailed
			case records[0].ParentID == "":
				root, found = records[0], true
				return nil
			}
			at = records[0].ParentID
		}
	})
	return root, found, err
}

// mcpTargetOpenPageDiscussion is the discussion a comment given to the page
// alone goes into (mcpOpenPageDiscussion), found among the target's first
// comments on no block, open, the latest first.
func mcpTargetOpenPageDiscussion(app core.App, authority mcpAuthority, target nativeCommentTarget) (commentRecord, bool, error) {
	root, found := commentRecord{}, false
	err := readCommentTargetSnapshot(app, target, mcpCommentReadAuthorizer(authority, target), func(txApp core.App, workspaceID string) error {
		return eachWindow(mcpOpenPageDiscussionWindow, func(size int, last *commentRecord) ([]commentRecord, error) {
			before := commentRecord{}
			if last != nil {
				before = *last
			}
			return readCommentTargetWindow(txApp, workspaceID, target, `
				AND [[block_id]]='' AND [[parent_id]]='' AND [[resolved]]=0
				AND ({:beforeCreated}='' OR [[created_at]]<{:beforeCreated}
					OR ([[created_at]]={:beforeCreated} AND [[id]]<{:beforeId}))
				ORDER BY [[created_at]] DESC,[[id]] DESC`, dbx.Params{"beforeCreated": before.CreatedAt, "beforeId": before.ID}, size)
		}, func(batch []commentRecord) (bool, error) {
			root, found = mcpOpenPageDiscussion(batch)
			return found, nil
		})
	})
	return root, found, err
}

// mcpOpenPageDiscussion is the discussion a comment given to the page alone
// goes into, as Notion's MCP and its page's comment box put one (seen
// 2026-11-11): the page's own open discussion, the latest started where
// there are more; none when all are resolved, so a new one starts. One on a
// database row's property is that property's, not the page's.
func mcpOpenPageDiscussion(records []commentRecord) (commentRecord, bool) {
	latest, found := commentRecord{}, false
	for _, record := range records {
		if record.ParentID != "" || record.BlockID != "" || record.Resolved {
			continue
		}
		anchor := struct {
			Quote      string `json:"quote"`
			PropertyID string `json:"propertyId"`
		}{}
		if json.Unmarshal(record.Body, &anchor) != nil || anchor.Quote != "" || anchor.PropertyID != "" {
			continue
		}
		if !found || record.CreatedAt > latest.CreatedAt ||
			record.CreatedAt == latest.CreatedAt && record.ID > latest.ID {
			latest, found = record, true
		}
	}
	return latest, found
}
