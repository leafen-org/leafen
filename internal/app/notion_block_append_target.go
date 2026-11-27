package app

import "github.com/pocketbase/pocketbase/core"

// notionBlockAppendTarget is where an append puts blocks: a page's body, a
// database page's, or a block's children.
type notionBlockAppendTarget struct {
	Identity   notionOAuthBearerIdentity
	Page       pageAuthorization
	PageID     string
	Owner      string
	ParentID   string
	ParentType string
}

// notionAppendUnderOriginal sends an append under a synced copy to the
// copy's original, as Notion's API does (seen 2026-11-21, Notion-Version
// 2026-03-11, synthetic pages under 한지테스트페이지: blocks appended to a copy
// on the original's page or another, at the end or the start, go under the
// original, every copy reads them, and the answer reads the original's
// children from the first of them, each naming the original as its parent;
// an empty append reads the original's children). Its place was checked
// against the copy, which holds no blocks of its own: an after_block naming
// one of the original's is refused as not the copy's, and so is one an
// earlier append left under the copy. An original gone is not found, named,
// and one under a block in the trash refuses it (notionSyncedCopyOriginalMissing).
// answered is true when it answered the append instead.
func notionAppendUnderOriginal(
	event *core.RequestEvent,
	target notionBlockAppendTarget,
	position *notionAppendPosition,
	afterID *string,
) (notionBlockAppendTarget, bool, error) {
	if target.ParentType != graphBlockTypeSyncedBlock {
		return target, false, nil
	}
	copied, found, err := loadStoredNotionBlock(event.App, target.ParentID)
	if err != nil {
		return target, true, notionRequestNotCompleted(event, err)
	}
	sourceID := notionSyncedCopySourceID(copied)
	if !found || sourceID == "" {
		return target, false, nil
	}
	if afterID != nil && position != nil && position.AfterBlock != nil {
		return target, true, notionBodyAnchorError(event, errNotionBodyAnchorOutside, position.AfterBlock.ID)
	}
	publicID, found, err := notionSyncedCopyOriginal(event.App, target.Page, sourceID)
	if err != nil || !found {
		return target, true, notionSyncedCopyOriginalMissing(event, target.Identity, sourceID, publicID, err, true)
	}
	original, authorized, err := authorizeNotionBlock(event, publicID, "pages:write", permissionWorkspaceWrite)
	if err != nil || !authorized {
		return target, true, err
	}
	return notionBlockAppendTarget{
		Identity: original.Identity, Page: original.Page,
		PageID: original.Block.PageID, Owner: original.Block.Owner,
		ParentID: original.Block.ID, ParentType: original.Block.Type,
	}, false, nil
}
