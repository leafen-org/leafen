package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type notionPageMarkdownUpdateRequest struct {
	Type                string                               `json:"type"`
	AllowAsync          bool                                 `json:"allow_async,omitempty"`
	InsertContent       *notionMarkdownInsertContentRequest  `json:"insert_content,omitempty"`
	ReplaceContentRange *notionMarkdownReplaceRangeRequest   `json:"replace_content_range,omitempty"`
	UpdateContent       *notionMarkdownUpdateContentRequest  `json:"update_content,omitempty"`
	ReplaceContent      *notionMarkdownReplaceContentRequest `json:"replace_content,omitempty"`
	// MarkdownVersion is the version the request writes and is answered in
	// (notionMarkdownVersion2).
	MarkdownVersion string `json:"markdown_version,omitempty"`
}

type notionMarkdownInsertContentRequest struct {
	Content  string                        `json:"content"`
	After    string                        `json:"after,omitempty"`
	Position *notionMarkdownInsertPosition `json:"position,omitempty"`
}

type notionMarkdownReplaceRangeRequest struct {
	Content              string `json:"content"`
	ContentRange         string `json:"content_range"`
	AllowDeletingContent bool   `json:"allow_deleting_content,omitempty"`
}

type notionMarkdownContentUpdate struct {
	OldString         string `json:"old_str"`
	NewString         string `json:"new_str"`
	ReplaceAllMatches bool   `json:"replace_all_matches,omitempty"`
}

type notionMarkdownUpdateContentRequest struct {
	ContentUpdates       []notionMarkdownContentUpdate `json:"content_updates"`
	AllowDeletingContent bool                          `json:"allow_deleting_content,omitempty"`
}

type notionMarkdownReplaceContentRequest struct {
	NewString            string `json:"new_str"`
	AllowDeletingContent bool   `json:"allow_deleting_content,omitempty"`
}

func serveNotionPageMarkdownUpdate(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	publicPageID string,
) error {
	authorization, authorized, err := authorizeNotionPageOrItemBodyAs(
		event, publicPageID, "pages:write", permissionWorkspaceWrite, notionTrashedMarkdownEdit,
	)
	if err != nil || !authorized {
		return err
	}
	input := notionPageMarkdownUpdateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	if refused, err := refuseNotionTrashedEdit(event, authorization.Page.Page.Id, true, false, notionTrashedMarkdownEdit); refused {
		return err
	}
	notionMarkdownAnchorBlocksFor(event, authorization, publicPageID, &input)
	if input.AllowAsync {
		return serveNotionAsyncWrite(event, authorization.Identity, notionAsyncOperationPageMarkdown, func() error {
			return updateNotionPageMarkdown(event, ygo, authorization, publicPageID, input)
		})
	}
	return updateNotionPageMarkdown(event, ygo, authorization, publicPageID, input)
}

// writeNotionCreatedPageMarkdown writes the first body of a page made just
// now, in the Markdown version its request names.
func writeNotionCreatedPageMarkdown(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	pageID, markdown, markdownVersion string,
) error {
	reader, err := notionIdentityMarkdownReader(event, identity, markdownVersion)
	if err != nil {
		return err
	}
	publicPageID, err := notionPagePublicID(event.App, pageID)
	if err != nil {
		return err
	}
	request := notionCreatedPageMarkdownWrite(identity, reader, pageID, markdown)
	request.PublicPageID, request.MutationID = publicPageID, "notion-public-create-markdown:"+newDiagnosticReference()
	_, err = ygo.replacePageMarkdown(event.Request.Context(), event.App, request)
	return err
}

// notionCreatedBodyEmptyCodeInput writes the body of a page just made with
// Markdown — one written after the page, as it makes sub-pages or databases —
// as its create writes any: code of nothing without text
// (notionPageCreateEmptyCode; seen 2026-11-14, Notion's API and MCP alike:
// "```⏎```" beside "<page>…</page>" made rich text []), then as prepare does.
func notionCreatedBodyEmptyCodeInput(
	prepare func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error),
) func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error) {
	return func(app core.App, current pageAuthorization, doc *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
		// Such code's text is written as none, each update kept in its
		// place: the steps around may hold others after them by index.
		kept := map[string]bool{}
		for _, update := range notionPageCreateEmptyCode(notionMarkdownPlan{
			Operations: input.Operations, TextUpdates: slices.Clone(input.TextUpdates),
		}) {
			kept[update.BlockID] = true
		}
		updates := slices.Clone(input.TextUpdates)
		for index := range updates {
			if updates[index].BlockID != "" && !kept[updates[index].BlockID] {
				updates[index].Spans = []graphTextSpan{}
			}
		}
		input.TextUpdates = updates
		if prepare == nil {
			return input, nil
		}
		return prepare(app, current, doc, input)
	}
}

func updateNotionPageMarkdown(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	authorization notionPageAuthorization,
	publicPageID string,
	input notionPageMarkdownUpdateRequest,
) error {
	reader, err := notionIdentityPageMarkdownReader(event, authorization.Identity, input.MarkdownVersion, authorization.Page.Page.Id, publicPageID)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	if !reader.Version2 {
		input = notionMarkdownVersion1CodeLinesInput(input)
	} else {
		input = notionMarkdownVersion2CodeLinesInput(input)
	}
	// What update_content left out is said first, before what the write kept as text.
	matchWarnings := []notionMarkdownWarning{}
	result, err := ygo.replacePageMarkdown(event.Request.Context(), event.App, pageMarkdownReplacement{
		PrincipalID: authorization.Identity.PrincipalID, PublicPageID: publicPageID, Reader: reader, Version2: reader.Version2,
		PrepareInput: notionMarkdownPublicMentionInput(authorization.Identity),
		Uploads: func(app core.App, _, uploadID string) (string, error) {
			content, err := resolveNotionCompletedFileUpload(app, authorization.Identity, uploadID)
			return content.FileID, err
		},
		Allows: func(app core.App, page pageAuthorization) bool {
			return notionOAuthIdentityAllowsPage(app, authorization.Identity, page)
		},
		MutationID:     "notion-public-markdown-update:" + newDiagnosticReference(),
		Authorize:      authorizeNotionPageMutation(authorization.Identity, authorization.Page.Page.Id),
		SyncedOriginal: notionSyncedOriginalWrite(authorization.Identity),
		Next: func(current string) (string, bool, error) {
			next, allowDeleting, warnings, err := applyNotionMarkdownChange(current, input)
			matchWarnings = warnings
			return next, allowDeleting, err
		},
	})
	requestError, planError, matchError := pageMarkdownRequestError{}, pageMarkdownPlanError{}, pageMarkdownMatchError{}
	switch {
	case errors.As(err, &matchError):
		return notionCompatibilityErrorWithData(event, http.StatusBadRequest, notionErrorValidation, matchError.message, matchError.data(), nil)
	// A change the page's Markdown can't take, and one that would delete
	// sub-pages or databases without allow_deleting_content, are what
	// Notion's reference answers validation_error, not a conflict.
	case errors.As(err, &requestError), errors.Is(err, errPageMarkdownUnsafe):
		return notionPageValidationError(event, err.Error())
	case errors.As(err, &planError) && !strings.HasPrefix(strings.TrimSpace(planError.message), notionFileUploadMissingPrefix):
		return notionCompatibilityErrorWithData(event, http.StatusBadRequest, notionErrorValidation,
			strings.TrimSpace(planError.message), planError.data(), nil)
	case errors.As(err, &planError):
		return notionPageValidationError(event, err.Error())
	case errors.Is(err, errNotionSyncedOriginalNotFound):
		return notionBlockNotFound(event)
	case err != nil:
		return notionPageMarkdownMutationError(event, err)
	}
	return writeNotionPageMarkdown(event, authorization.Page.Page.Id, publicPageID, result.After, append(matchWarnings, result.Warnings...))
}

// pageMarkdownReplacement replaces a page's body with Markdown made from its
// current Markdown, as one content graph change the given admission allows.
type pageMarkdownReplacement struct {
	PrincipalID  string
	PublicPageID string
	MutationID   string
	// Reader reads the body's Markdown before and after.
	Reader notionMarkdownReader
	// Version2 reads the new Markdown as the public API's "v2" writes do (notionMarkdownVersion2Input).
	Version2 bool
	// CreatesPage writes a new page's first body, whose synced references may name a page (synced_block_page_original.go).
	CreatesPage bool
	// PrepareInput checks the new blocks' mentions as the writer names them.
	PrepareInput func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error)
	// Uploads finds the file an upload of the writer's connection made, for
	// a file-upload:// line; nil places none.
	Uploads func(app core.App, workspaceID, uploadID string) (string, error)
	// Allows tells whether the writer may reach a page the Markdown makes; nil allows every page the principal may write.
	Allows func(core.App, pageAuthorization) bool
	// RequestHash, when set, tells a replay of one request from another
	// request under the same mutation ID.
	RequestHash    string
	BasePageCursor *int
	// BaseVersion, when set, is the page's write version the writer read
	// (pageWriteVersion), which the body must still be at when it is read
	// for the change; the change then holds to that read.
	BaseVersion *int
	// BaseGraphRevision, when set, is the body's revision the writer read,
	// which the write must still find; nil takes the body as it reads now.
	BaseGraphRevision *int
	// SkipUnchanged leaves a body whose Markdown the change would not alter.
	SkipUnchanged bool
	Authorize     func(core.App) (pageAuthorization, error)
	// SyncedOriginal, when set, lets a synced block reference holding blocks
	// write them into its original on another page: that page's public ID
	// and the admission its write takes (notion_page_markdown_synced_content.go).
	SyncedOriginal func(app core.App, pageID string) (string, func(core.App) (pageAuthorization, error), error)
	// Next is the body's new Markdown, from its current Markdown, and whether
	// content that Markdown can't hold may go.
	Next func(current string) (string, bool, error)
}

// readBefore is the body before as the plan matches its blocks: its
// Markdown read as a write in its form is, the public API's "v1" by its own
// rules but a colored equation's closing line its writer writes
// (notionMarkdownVersion1OwnInput), "v2"'s with the indents its text blocks'
// tags hold and its inline code over lines on one (notionMarkdownVersion2Before),
// so that the lines it writes read back as the blocks they came from.
func (request pageMarkdownReplacement) readBefore(before notionPageMarkdownProjection) notionPageMarkdownProjection {
	if request.Version2 {
		before.Markdown, before.Lines = notionMarkdownVersion2Before(before.Markdown), notionMarkdownVersion2Before(before.Lines)
	}
	if !request.Version2 && request.Reader.APIForm {
		before.Markdown = notionMarkdownVersion1OwnInput(before.Markdown)
		if before.Lines != "" {
			before.Lines = notionMarkdownVersion1OwnInput(before.Lines)
		}
	}
	// The addresses of this server's pages the writer writes alone are the
	// pages' mentions, named as its reader names them.
	before.SitePages, before.Version1 = request.Reader.sitePages(), !request.Version2 && request.Reader.APIForm
	return before
}

// pageMarkdownReplaced is a page body before and after its replacement; a
// replayed request leaves After empty.
type pageMarkdownReplaced struct {
	Before, After notionPageMarkdownProjection
	Receipt       graphMutationReceipt
	// Warnings are what a "v2" write changed or kept as text.
	Warnings []notionMarkdownWarning
}

var errPageMarkdownUnsafe = errors.New(
	"The page holds content that replacing it would delete; set allow_deleting_content to replace it anyway.",
)

func (p *pocketBaseYgoPersistence) replacePageMarkdown(
	ctx context.Context,
	app core.App,
	request pageMarkdownReplacement,
) (pageMarkdownReplaced, error) {
	result := pageMarkdownReplaced{}
	var page *core.Record
	written := pageAuthorization{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := request.Authorize(txApp)
		if err != nil {
			return err
		}
		page, written = current.Page, current
		result.Before, err = projectNotionPageMarkdown(
			txApp, current.Workspace.OwnerID, current.Page.Id, request.PublicPageID, request.Reader,
		)
		if err == nil && request.RequestHash != "" {
			// A replay answers as the change it repeats did, whatever the body
			// holds now, before any of it is planned again.
			result.Receipt, _, err = storedGraphMutationReplay(
				txApp, current.Workspace.OwnerID, current.Page.Id, request.MutationID, request.RequestHash,
			)
		}
		if err != nil || result.Receipt.Duplicate {
			return err
		}
		return holdPageWriteVersion(txApp, current.Page, result.Before, request.BaseVersion)
	})
	if err != nil || result.Receipt.Duplicate {
		return result, err
	}
	markdown, allowDeleting, err := request.Next(result.Before.Markdown)
	if matchError := (pageMarkdownMatchError{}); errors.As(err, &matchError) {
		return result, matchError
	}
	if err != nil {
		return result, pageMarkdownRequestError{message: err.Error()}
	}
	// Read as its plan reads it (notion_page_markdown_write_check.go).
	markdown, synced, warnings, err := request.readWritten(app, written, result.Before, markdown)
	if err != nil {
		return result, err
	}
	result.Warnings = warnings
	if request.SkipUnchanged && sameMarkdownBlocksUntouched(markdown, request.readBefore(result.Before)) {
		changed, err := p.writeNotionMarkdownSyncedOriginals(ctx, app, request, synced, allowDeleting, &result)
		result.After = result.Before
		result.Receipt = graphMutationReceipt{MutationID: request.MutationID, Duplicate: !changed}
		return result, err
	}
	// A body too long to read whole can't be written back.
	if result.Before.Truncated {
		return result, errPageMarkdownUnsafe
	}
	before := request.readBefore(result.Before)
	plan, err := notionMarkdownReplacementPlan(markdown, &before, allowDeleting)
	if errors.Is(err, errPageMarkdownUnsafe) {
		return result, err
	}
	if err != nil {
		return result, notionMarkdownPlanFailure(err)
	}
	// What it makes before its blocks — the originals its references change,
	// the full-page databases its tags ask for — waits for them to pass.
	if len(synced) > 0 || len(plan.FullPageDatabases) > 0 {
		if err := request.checkBeforeMaking(app, before, markdown, allowDeleting); err != nil {
			return result, err
		}
	}
	// The originals the references give blocks are written once the write
	// here is known to take, and before it, which then reads them.
	if _, err := p.writeNotionMarkdownSyncedOriginals(ctx, app, request, synced, allowDeleting, &result); err != nil {
		return result, err
	}
	if len(plan.FullPageDatabases) > 0 {
		// The full-page databases the Markdown makes come first; their tags
		// then name them.
		if markdown, err = p.makeNotionMarkdownDatabases(ctx, app, request, markdown, plan.FullPageDatabases, &result.Before); err != nil {
			return result, err
		}
		before = request.readBefore(result.Before)
		if plan, err = notionMarkdownReplacementPlan(markdown, &before, allowDeleting); errors.Is(err, errPageMarkdownUnsafe) {
			return result, err
		} else if err != nil {
			return result, notionMarkdownPlanFailure(err)
		}
		if len(plan.FullPageDatabases) > 0 {
			return result, pageMarkdownPlanError{message: "Markdown content could not be planned."}
		}
		page, err = app.FindRecordById(pagesCollection, page.Id)
		if err != nil {
			return result, err
		}
	}
	// The titles tags give, their mentions named before anything is written.
	if plan.Renames, err = request.preparedRenames(app, plan.Renames); err != nil {
		return result, err
	}
	cursor, graphRevision := page.GetInt("cursor"), result.Before.GraphRevision
	if request.BasePageCursor != nil {
		cursor = *request.BasePageCursor
	}
	if request.BaseGraphRevision != nil {
		graphRevision = *request.BaseGraphRevision
	}
	hooks := contentGraphMutationHooks{
		// Page blocks follow their pages, as a person's change keeps them.
		PersonChange:          true,
		ExplicitStartBlockIDs: plan.StartBlockIDs,
		// A changed line takes its block's text; one left as it is, the
		// pieces its line was read in.
		ReplaceExistingText: true,
		TextPieces:          plan.TextPieces,
		// Its other steps place files in its operations and read the page
		// back: search and the page's readers take what its operations named.
		SearchFromNamedBlocks: true, StepsKeepGraph: true,
		PrepareInput: func(txApp core.App, current pageAuthorization, doc *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
			return request.preparedBlocks(txApp, plan, current, doc, input)
		},
		Authorize: request.Authorize,
		AfterApply: func(txApp core.App, current pageAuthorization, _ *crdt.Doc) error {
			var err error
			result.After, err = projectNotionPageMarkdown(
				txApp, current.Workspace.OwnerID, current.Page.Id, request.PublicPageID, request.Reader,
			)
			return err
		},
	}
	if request.RequestHash != "" {
		hooks.ResolveRequestHash = func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
			return request.RequestHash, nil
		}
	}
	if len(plan.Operations) == 0 && len(plan.TextUpdates) == 0 {
		// Markdown that changes no block leaves the body as it is, but for
		// the pieces of its texts.
		if err := keepNotionMarkdownTextPieces(ctx, app, page, request.PrincipalID, plan.TextPieces); err != nil {
			return result, err
		}
		result.After = result.Before
		result.Receipt = graphMutationReceipt{MutationID: request.MutationID}
	} else {
		result.Receipt, err = p.applyContentGraphMutationContext(ctx, request.PrincipalID, page.Id, graphMutationInput{
			MutationID: request.MutationID, Operations: plan.Operations, TextUpdates: plan.TextUpdates,
			BasePageCursor: &cursor, BaseGraphRevision: &graphRevision,
		}, hooks)
		if err != nil || result.Receipt.Duplicate {
			return result, err
		}
	}
	if len(plan.Renames) == 0 {
		return result, nil
	}
	// Sub-pages take the titles their tags gave once the body is written,
	// and full-page databases on their first data source too.
	if err := p.renameNotionMarkdownTagPages(ctx, app, request.PrincipalID, plan.Renames); err != nil {
		return result, err
	}
	for _, rename := range plan.Renames {
		if rename.SourceID == "" {
			continue
		}
		if err := p.renameNotionMarkdownDatabaseSource(ctx, request.PrincipalID, rename.PageID, rename.SourceID, rename.Title, rename.Spans); err != nil {
			return result, err
		}
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := request.Authorize(txApp)
		if err != nil {
			return err
		}
		result.After, err = projectNotionPageMarkdown(
			txApp, current.Workspace.OwnerID, current.Page.Id, request.PublicPageID, request.Reader,
		)
		return err
	})
	return result, err
}

// applyNotionMarkdownChange is a page's new Markdown from its current one,
// whether content it can't hold may go, and what the change left out
// (notion_page_markdown_matches.go).
func applyNotionMarkdownChange(
	current string,
	input notionPageMarkdownUpdateRequest,
) (string, bool, []notionMarkdownWarning, error) {
	payloads := 0
	for _, present := range []bool{
		input.InsertContent != nil, input.ReplaceContentRange != nil,
		input.UpdateContent != nil, input.ReplaceContent != nil,
	} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return "", false, nil, errors.New("Exactly one Markdown update payload is required.")
	}
	switch input.Type {
	case "insert_content":
		value := input.InsertContent
		if value != nil && value.After != "" && value.Position != nil {
			return "", false, nil, pageMarkdownRequestError{message: notionMarkdownAfterWithPosition}
		} else if value == nil {
			return "", false, nil, errors.New("insert_content does not match the declared type.")
		}
		// It may add a synced block reference, not change its original.
		if refused := notionMarkdownSyncedRefusal(value.Content, input.MarkdownVersion == notionMarkdownVersion2, true); refused != "" {
			return "", false, nil, pageMarkdownMatchError{message: refused}
		}
		// Inserted content starts blocks of its own, as Notion's does: after
		// a selection it begins a new line, and what followed the selection
		// continues its last line; at the start the page's first block
		// begins after it.
		if value.After != "" {
			_, end, err := notionMarkdownSelection(current, value.After)
			if err != nil {
				return "", false, nil, err
			}
			return current[:end] + "\n" + value.Content + current[end:], false, nil, nil
		}
		position := "end"
		if value.Position != nil {
			position = value.Position.Type
		}
		if notionMarkdownAnchored(position) {
			return notionMarkdownInsertAnchored(current, input, value)
		}
		// In "v2" a line of text goes on with the one before it, so the
		// inserted blocks stand a blank line apart.
		join := notionMarkdownJoinBlocks
		if input.MarkdownVersion == notionMarkdownVersion2 {
			join = notionMarkdownJoinVersion2Blocks
		}
		if position == "start" {
			return join(value.Content, current), false, nil, nil
		}
		if position == "end" {
			return join(current, value.Content), false, nil, nil
		}
		return "", false, nil, errors.New("insert_content.position.type is invalid.")
	case "replace_content_range":
		value := input.ReplaceContentRange
		if value == nil {
			return "", false, nil, errors.New("replace_content_range does not match the declared type.")
		}
		start, end, err := notionMarkdownSelection(current, value.ContentRange)
		if err != nil {
			return "", false, nil, err
		}
		return current[:start] + value.Content + current[end:], value.AllowDeletingContent, nil, nil
	case "update_content":
		value := input.UpdateContent
		if value == nil {
			return "", false, nil, errors.New("update_content does not match the declared type.")
		}
		updated, warnings, err := applyNotionMarkdownUpdates(current, value.ContentUpdates)
		return updated, value.AllowDeletingContent, warnings, err
	case "replace_content":
		value := input.ReplaceContent
		if value == nil {
			return "", false, nil, errors.New("replace_content does not match the declared type.")
		}
		return value.NewString, value.AllowDeletingContent, nil, nil
	default:
		return "", false, nil, errors.New("The Markdown update type is invalid.")
	}
}

// notionMarkdownJoinVersion2Blocks puts one "v2" Markdown text's blocks
// after another's, a blank line between them.
func notionMarkdownJoinVersion2Blocks(first, second string) string {
	if strings.TrimSpace(first) == "" || strings.TrimSpace(second) == "" {
		return first + second
	}
	return strings.TrimRight(first, "\n") + "\n\n" + strings.TrimLeft(second, "\n")
}

func notionPageMarkdownMutationError(event *core.RequestEvent, err error) error {
	if isDLPPolicyDecisionError(err) {
		return notionDLPPolicyError(event, err)
	}
	requestError := &graphRequestError{}
	if errors.As(err, &requestError) {
		switch requestError.status {
		case http.StatusNotFound:
			return notionPageNotFound(event)
		case http.StatusBadRequest:
			return notionPageValidationError(event, requestError.message)
		case http.StatusConflict:
			return notionGraphConflictError(event, requestError)
		}
	}
	return notionRequestNotCompleted(event, err)
}

// sameMarkdownBlocks tells whether Markdown makes the same blocks as the
// body's, read as the body reads Markdown written over it: the same types,
// text, marks and content under the same parents.
func sameMarkdownBlocks(markdown string, before notionPageMarkdownProjection) bool {
	leftItems, leftErr := parseNotionMarkdown(markdown, before.KeepLineEnds, before.SitePages)
	rightItems, rightErr := parseNotionMarkdown(before.Markdown, before.KeepLineEnds, before.SitePages)
	if leftErr != nil || rightErr != nil || len(leftItems) != len(rightItems) {
		return false
	}
	shape := func(items []workspaceImportStagedItem) []string {
		index := make(map[string]int, len(items))
		for position, item := range items {
			index[item.ID] = position
		}
		shapes := make([]string, len(items))
		for position, item := range items {
			parent := -1
			if at, ok := index[item.ParentID]; ok {
				parent = at
			}
			spans, _ := json.Marshal(item.Spans)
			shapes[position] = fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s", parent, item.Type, item.Text, spans, item.Content)
		}
		return shapes
	}
	return slices.Equal(shape(leftItems), shape(rightItems))
}
