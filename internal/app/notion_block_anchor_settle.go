package app

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// notionBlockWriteAnchors keeps the sub-pages hanging on a page's lines,
// table rows and captioned blocks (mention_anchored_pages.go,
// mention_anchored_fields.go) with those blocks when an integration writes,
// deletes or restores them through the Public API, as Notion's API does
// (seen twice 2026-11-18, Notion-Version 2022-06-28): a deleted block takes
// the pages hanging on it to the trash, its mention of them still there or
// already gone, and the block restored brings back those it still mentions
// and those that went with it unmentioned. A mention an integration takes
// out of a block that stays leaves its page where it is, hanging there
// unmentioned (mention_anchored_unmentioned.go): the page stays out of the
// trash, still under that block, and out of the sidebar until a mention of
// it is back.
type notionBlockWriteAnchors struct {
	actorID                string
	written, deleted, made []string
}

func newNotionBlockWriteAnchors(actorID string) *notionBlockWriteAnchors {
	return &notionBlockWriteAnchors{actorID: actorID}
}

// noting notes, before the write is made, the blocks it writes, those it
// deletes (with all they hold) and those it makes again, then runs next.
func (anchors *notionBlockWriteAnchors) noting(
	next func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error,
) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return func(app core.App, current pageAuthorization, doc *crdt.Doc, operations []graphOperation) error {
		view, parents := newStructuralMetadataView(doc), childPageChangeParents{}
		for _, operation := range operations {
			switch {
			case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil:
				if _, exists, err := view.get(operation.Block.ID); err != nil {
					return err
				} else if !exists {
					anchors.made = append(anchors.made, operation.Block.ID)
				} else {
					anchors.written = append(anchors.written, operation.Block.ID)
				}
			case operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil:
				_, ids, err := parents.deleted(view, operation.Deletion.BlockID)
				if err != nil {
					return err
				}
				anchors.deleted = append(anchors.deleted, ids...)
			}
			parents.note(operation)
		}
		if next == nil {
			return nil
		}
		return next(app, current, doc, operations)
	}
}

// settling runs next, then, once the write is in, sends the pages hanging
// on the deleted blocks to the trash, brings back those of the blocks made
// again and marks those of the written blocks as mentioned there or not,
// placing those back in the sidebar where their blocks stand.
func (anchors *notionBlockWriteAnchors) settling(
	next func(core.App, pageAuthorization, *crdt.Doc) error,
) func(core.App, pageAuthorization, *crdt.Doc) error {
	return func(app core.App, current pageAuthorization, doc *crdt.Doc) error {
		if next != nil {
			if err := next(app, current, doc); err != nil {
				return err
			}
		}
		if len(anchors.written) == 0 && len(anchors.deleted) == 0 && len(anchors.made) == 0 {
			return nil
		}
		pageID := current.Page.Id
		placed, err := settleAnchoredPages(app, anchors.actorID, pageID, "notion-block-anchor:"+newDiagnosticReference(),
			anchorChange{blocks: slices.Concat(anchors.deleted, anchors.written), made: anchors.made, kept: true})
		if err != nil || len(placed) == 0 {
			return err
		}
		metadata, err := structuralMetadata(doc)
		if err != nil {
			return err
		}
		return alignChildPageSidebar(
			app, doc, metadata, current.Workspace.OwnerID, current.Workspace.WorkspaceID, pageID, placed,
		)
	}
}
