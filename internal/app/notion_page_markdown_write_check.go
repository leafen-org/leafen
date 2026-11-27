package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's Markdown write refuses what its blocks name and can't be — a link
// to no page it may open, a synced block reference to no original, a button
// line naming no button it keeps, an upload that isn't there, a mention it
// can't make — as its one content graph change prepares them
// (preparedBlocks). Some writes make things before that change: the synced
// block originals its references change, the full-page databases its
// <database> tags ask for, and, for a page made with a body that makes
// sub-pages or databases or names blocks by address, the page itself, its
// body written once it is there (notion-create-pages, the public API's
// POST /v1/pages, a database's page alike). Those wait until the blocks are
// checked as that change will prepare them, so a write refused for its
// blocks leaves nothing made, as Notion's refused writes leave nothing (seen
// 2026-11-27, the public API's "v1" and "v2": a refused create made no page,
// a sub-page or a database in its body or not, and a refused update no
// database). Before (2026-11-27) such a create left its page, titled and
// empty, a database's item, the pages its body moved in moved into it, and
// the pages before it in the same call; an update left its new databases.

// readWritten is the new Markdown as the write's plan reads it: new
// <unknown> tags refused first, as written (notionMarkdownUpdateNewUnknown),
// the synced block originals it changes found (notionMarkdownSyncedOriginals),
// then read in its version, with what that said, and its unknown blocks paired
// with those before.
func (request pageMarkdownReplacement) readWritten(
	app core.App, written pageAuthorization, before notionPageMarkdownProjection, markdown string,
) (string, []notionMarkdownSyncedWrite, []notionMarkdownWarning, error) {
	var err error
	if request.Version2 || request.Reader.APIForm {
		// What a synced reference holds is its original's, not this page's (notion_page_markdown_synced_page.go).
		if err = notionMarkdownUpdateNewUnknown(notionMarkdownWithoutSyncedHeld(markdown, request.Version2), before.UnknownBlocks, request.Version2); err != nil {
			return "", nil, nil, notionMarkdownPlanFailure(err)
		}
	}
	synced := []notionMarkdownSyncedWrite{}
	if request.SyncedOriginal != nil {
		if markdown, synced, err = notionMarkdownSyncedOriginals(app, request, written, before.Markdown, markdown); err != nil {
			return "", nil, nil, err
		}
	}
	var warnings []notionMarkdownWarning
	if request.Version2 {
		if markdown, warnings, err = notionMarkdownVersion2Input(markdown); err != nil {
			return "", nil, nil, notionMarkdownPlanFailure(err)
		}
	} else if request.Reader.APIForm {
		markdown = notionMarkdownVersion1Input(notionMarkdownVersion1ReadUnknownLines(notionMarkdownVersion1EmptiedUnknownItems(markdown)))
	}
	if request.Version2 || request.Reader.APIForm {
		if markdown, err = notionMarkdownPairedUnknown(markdown, before.UnknownBlocks, request.Version2); err != nil {
			return "", nil, nil, notionMarkdownPlanFailure(err)
		}
	}
	return markdown, synced, warnings, nil
}

// preparedBlocks are the blocks a plan writes as the write's content graph
// change takes them, current and doc being the page written and its blocks:
// a :name: no custom emoji has is words that stand apart, one a custom emoji
// has changes its text; its files placed, the blocks it names by address
// pointed at what they name, and its mentions checked as the writer names
// them, those of the titles tags give databases too.
func (request pageMarkdownReplacement) preparedBlocks(
	txApp core.App, plan notionMarkdownPlan, current pageAuthorization, doc *crdt.Doc, input graphMutationInput,
) (graphMutationInput, error) {
	if err := resolveNotionMarkdownCustomEmojis(txApp, current.Workspace.WorkspaceID, plan.TextPieces); err != nil {
		return graphMutationInput{}, err
	}
	input.TextUpdates = notionMarkdownKeptTextUpdates(plan.TextPieces, input.TextUpdates, input.Operations)
	files := notionMarkdownFiles{WorkspaceID: current.Workspace.WorkspaceID, OnPage: plan.PageFiles, Upload: request.Uploads}
	if err := files.place(txApp, input.Operations); err != nil {
		return graphMutationInput{}, err
	}
	if err := request.addressedBlocks(txApp, current, doc, input.Operations); err != nil {
		return graphMutationInput{}, err
	}
	if request.PrepareInput == nil {
		return input, nil
	}
	return prepareNotionMarkdownSourceTitles(input, func(input graphMutationInput) (graphMutationInput, error) {
		return request.PrepareInput(txApp, current, doc, input)
	})
}

// checkBlocks refuses Markdown whose blocks the write's change would refuse,
// read as the write reads them against before, current and doc being the
// page written and its blocks — for a page not yet made, its workspace and
// none. A <database> tag that asks for a full-page database stands as a new
// sub-page's for this: the database is made only once the blocks pass. What
// it prepares is read and left, the write preparing its own.
func (request pageMarkdownReplacement) checkBlocks(
	app core.App, current pageAuthorization, doc *crdt.Doc, before notionPageMarkdownProjection, markdown string, allowDeleting bool,
) error {
	plan, err := notionMarkdownReplacementPlan(notionMarkdownDatabasesAsPages(markdown), &before, allowDeleting)
	if errors.Is(err, errPageMarkdownUnsafe) {
		return err
	}
	if err != nil {
		return notionMarkdownPlanFailure(err)
	}
	request.Authorize = func(core.App) (pageAuthorization, error) { return current, nil }
	if _, err := request.preparedRenames(app, plan.Renames); err != nil {
		return err
	}
	_, err = request.preparedBlocks(app, plan, current, doc, graphMutationInput{
		Operations: copiedGraphOperations(plan.Operations), TextUpdates: slices.Clone(plan.TextUpdates),
	})
	return err
}

// checkBeforeMaking refuses, before the write makes anything ahead of its
// blocks — the originals its references change, the full-page databases its
// tags ask for — Markdown whose blocks it would then refuse, read against the
// page as it is.
func (request pageMarkdownReplacement) checkBeforeMaking(
	app core.App, before notionPageMarkdownProjection, markdown string, allowDeleting bool,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := request.Authorize(txApp)
		if err != nil {
			return err
		}
		doc, _, err := loadYgoStructuralDoc(txApp, current.Page.Id)
		if err != nil {
			return err
		}
		return request.checkBlocks(txApp, current, doc, before, markdown, allowDeleting)
	})
}

// checkCreatedBody refuses a new page's body that its write, once the page is
// made, would refuse: read from its Markdown (Next) as that write reads it on
// a page of no blocks but the pages it moves in (references), current being
// the page just made, or the workspace standing for one not yet made. A new
// page's references change no original (a create refuses one that would).
func (request pageMarkdownReplacement) checkCreatedBody(
	app core.App, current pageAuthorization, references []notionMarkdownReference,
) error {
	request.SyncedOriginal = nil
	markdown, allowDeleting, err := request.Next("")
	if err != nil {
		return pageMarkdownRequestError{message: err.Error()}
	}
	empty := notionPageMarkdownProjection{References: references}
	if markdown, _, _, err = request.readWritten(app, current, empty, markdown); err != nil {
		return err
	}
	return request.checkBlocks(app, current, crdt.New(), request.readBefore(empty), markdown, allowDeleting)
}

// notionMarkdownDatabasesAsPages is Markdown with each tag that asks for a
// full-page database (notionMarkdownFullPageDatabaseLines) a new sub-page's
// of its title in its place, as far in.
func notionMarkdownDatabasesAsPages(markdown string) string {
	lines := strings.Split(notionMarkdownContainerForms(markdown), "\n")
	tagLines := notionMarkdownFullPageDatabaseLines(lines)
	if len(tagLines) == 0 {
		return markdown
	}
	for _, index := range tagLines {
		line := lines[index]
		tag := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "<database"), "</database>")
		lines[index] = line[:len(line)-len(strings.TrimLeft(line, " \t"))] + "<page" + tag + "</page>"
	}
	return strings.Join(lines, "\n")
}
