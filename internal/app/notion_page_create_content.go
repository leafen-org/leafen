package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type notionPageCreateContentPlan struct {
	Operations       []graphOperation
	TextUpdates      []graphTextUpdate
	AdmissionContent string
	BodyPlacement    *pageBodyPlacement
	// Kind is the new page's kind; empty is a page.
	Kind string
	// DeferredMarkdown is a body that makes sub-pages or databases, written
	// as a page's Markdown update writes it once the page is made; checked
	// first as the page is made (checkDeferred), from origin, in its version.
	DeferredMarkdown                string
	deferredOrigin, deferredVersion string
	// ContentRefused is the faults in the content of the blocks planned, in
	// Operations, said once what Notion's API says before them has passed
	// (notionPageCreateContentRefused).
	ContentRefused error
	// Markdown is whether the body was written as Markdown, whose mentions
	// are prepared as a page's Markdown update prepares them: a data
	// source's is the source's own, or kept by the ID written
	// (notion_markdown_data_source_mentions.go).
	Markdown bool
}

// notionPageCreateContentAsked reads how a page create request asks for its
// content: its template, its place, and its blocks, at most a hundred, or
// its Markdown. Notion says a fault here before it reads the page's title,
// and one in what the content holds after (seen 2026-11-10).
func notionPageCreateContentAsked(input notionPageCreateRequest) (*pageBodyPlacement, []json.RawMessage, error) {
	if err := validateNotionPageCreateTemplate(input.Template); err != nil {
		return nil, nil, err
	}
	placement, err := parseNotionPageCreatePosition(input.Position)
	if err != nil {
		return nil, nil, err
	}
	// Children given, even none, are the page's blocks and content given
	// beside them is left unread, as Notion's (seen 2026-11-10).
	blocks := input.Content
	if input.Children != nil {
		blocks = input.Children
	}
	if len(blocks) > 100 {
		return nil, nil, errors.New("Page content supports at most 100 root blocks.")
	}
	// Markdown goes with neither, even empty ones.
	if input.Markdown != nil && (input.Content != nil || input.Children != nil) {
		return nil, nil, errors.New("Give 'markdown', or 'content' or 'children', not both: one content format per request.")
	}
	if input.AllowAsync {
		return nil, nil, errors.New("allow_async is answered before the page content is planned")
	}
	return placement, blocks, nil
}

// planNotionPageCreateContent plans a new page's blocks, origin naming this
// server's pages in its Markdown; check, when given,
// finds the faults in their content only the workspace can tell
// (notionBlockAppendPlan.ContentCheck).
func planNotionPageCreateContent(
	input notionPageCreateRequest, check func(*graphBlock) string, origin string,
) (notionPageCreateContentPlan, error) {
	placement, blocks, err := notionPageCreateContentAsked(input)
	if err != nil {
		return notionPageCreateContentPlan{}, err
	}
	if input.Markdown != nil {
		markdown := *input.Markdown
		// A new page may hold a synced block reference, not change its original.
		if refused := notionMarkdownSyncedRefusal(markdown, input.MarkdownVersion == notionMarkdownVersion2, false); refused != "" {
			return notionPageCreateContentPlan{}, errors.New(refused)
		}
		// An <unknown/> an item holds is refused as its page's Markdown is
		// read, after what "v2"'s reading says (notionMarkdownCreateHeldUnknown).
		held := notionMarkdownCreateHeldUnknown(markdown, input.MarkdownVersion == notionMarkdownVersion2)
		if input.MarkdownVersion == notionMarkdownVersion2 {
			if markdown, _, err = notionMarkdownVersion2Input(markdown); err != nil {
				return notionPageCreateContentPlan{}, err
			}
		}
		if held != nil {
			return notionPageCreateContentPlan{}, held
		}
		if input.MarkdownVersion != notionMarkdownVersion2 {
			markdown = notionMarkdownCreateUnknownText(notionMarkdownVersion1Input(markdown))
		}
		plan, err := notionMarkdownReplacementPlan(markdown, &notionPageMarkdownProjection{
			KeepLineEnds: input.MarkdownVersion != notionMarkdownVersion2, SitePages: notionMarkdownSitePagesAt(origin, false),
		}, false)
		if unknown := (notionMarkdownNewUnknownError{}); errors.As(err, &unknown) {
			return notionPageCreateContentPlan{}, notionMarkdownCreateUnknownError{
				unknown: unknown, version2: input.MarkdownVersion == notionMarkdownVersion2,
			}
		}
		// A tag naming no block here says why its address makes none.
		if elsewhere := (notionMarkdownElsewhereError{}); errors.As(err, &elsewhere) {
			return notionPageCreateContentPlan{}, errors.New(elsewhere.created(origin))
		}
		if err != nil {
			return notionPageCreateContentPlan{}, err
		}
		if notionMarkdownMakesObjects(plan) {
			return notionPageCreateContentPlan{
				DeferredMarkdown: *input.Markdown, BodyPlacement: placement, deferredOrigin: origin, deferredVersion: input.MarkdownVersion,
			}, nil
		}
		plan.TextUpdates = notionPageCreateEmptyCode(plan)
		// Mentions name their targets as the connection does until the
		// page's mention preparation checks them.
		captions, put := notionMarkdownSpanFields(plan.Operations)
		if err := notionMarkdownPublicMentions(slices.Concat(plan.TextUpdates, captions)); err != nil {
			return notionPageCreateContentPlan{}, err
		}
		if err := put(); err != nil {
			return notionPageCreateContentPlan{}, err
		}
		return notionPageCreateContentPlan{
			Operations: plan.Operations, TextUpdates: plan.TextUpdates, AdmissionContent: *input.Markdown,
			BodyPlacement: placement, Markdown: true,
		}, nil
	}
	if len(blocks) == 0 {
		return notionPageCreateContentPlan{BodyPlacement: placement}, nil
	}
	plan := notionBlockAppendPlan{ContentCheck: check}
	if err := plan.appendRequestBlocks(blocks, "", "", nil); err != nil {
		if len(plan.ContentFaults) == 0 {
			return notionPageCreateContentPlan{}, err
		}
		return notionPageCreateContentPlan{Operations: plan.Operations, ContentRefused: err}, err
	}
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return notionPageCreateContentPlan{}, errors.New("Page content is invalid.")
	}
	return notionPageCreateContentPlan{
		Operations: plan.Operations, TextUpdates: plan.TextUpdates,
		AdmissionContent: string(encoded), BodyPlacement: placement,
	}, nil
}

// checkDeferred refuses, in the transaction that makes the page pageID, a
// body DeferredMarkdown whose write once the page is there would refuse it
// (pageMarkdownReplacement.checkCreatedBody), as identity writes it: so the
// page isn't made, as Notion's refused create makes none (seen 2026-11-27).
// nil for a plan that writes no body after.
func (plan notionPageCreateContentPlan) checkDeferred(app core.App, identity notionOAuthBearerIdentity, pageID string) error {
	if plan.DeferredMarkdown == "" {
		return nil
	}
	reader := notionIdentityMarkdownReaderAt(app, plan.deferredOrigin, identity, plan.deferredVersion)
	request := notionCreatedPageMarkdownWrite(identity, reader, pageID, plan.DeferredMarkdown)
	current, err := request.Authorize(app)
	if err != nil {
		return err
	}
	return request.checkCreatedBody(app, current, nil)
}

// notionCreatedPageMarkdownWrite is the write of the first body of a page
// made just now, pageID, by a public API connection: markdown read as
// reader reads it, its mentions, files and links as the connection names
// them (writeNotionCreatedPageMarkdown).
func notionCreatedPageMarkdownWrite(
	identity notionOAuthBearerIdentity, reader notionMarkdownReader, pageID, markdown string,
) pageMarkdownReplacement {
	return pageMarkdownReplacement{
		PrincipalID: identity.PrincipalID, Reader: reader, Version2: reader.Version2,
		PrepareInput: notionCreatedBodyEmptyCodeInput(notionMarkdownPublicMentionInput(identity)),
		Uploads: func(app core.App, _, uploadID string) (string, error) {
			content, err := resolveNotionCompletedFileUpload(app, identity, uploadID)
			return content.FileID, err
		},
		Allows: func(app core.App, page pageAuthorization) bool {
			return notionOAuthIdentityAllowsPage(app, identity, page)
		},
		Authorize: authorizeNotionPageMutation(identity, pageID), CreatesPage: true,
		Next: func(string) (string, bool, error) { return markdown, false, nil },
	}
}

// notionPageCreateEmptyCode is a new page's text updates without the empty
// piece of text its Markdown reads code of nothing as: a page made with
// Markdown has such code of rich text [], where a Markdown update of a page
// gives it [""] (seen 2026-11-14, "v1" and "v2" alike: "```⏎```", "x⏎```",
// "- ```", "```js"; graph_text_empty_pieces.go).
func notionPageCreateEmptyCode(plan notionMarkdownPlan) []graphTextUpdate {
	code := map[string]bool{}
	for _, operation := range plan.Operations {
		if operation.Block != nil && operation.Block.Type == graphBlockTypeCode {
			code[operation.Block.ID] = true
		}
	}
	return slices.DeleteFunc(plan.TextUpdates, func(update graphTextUpdate) bool {
		return code[update.BlockID] && len(update.Spans) == 1 && update.Spans[0].Text == "" &&
			update.Spans[0].EmptyPiece == graphTextEmptyText
	})
}

// parseNotionPageCreatePosition reads a new page's place in its parent's
// body, its shape checked before (notionPagePositionShape): by its type, and
// an after_block's ID, alone — Notion leaves a page_start's or page_end's
// value, and every other key (seen 2026-11-14).
func parseNotionPageCreatePosition(raw json.RawMessage) (*pageBodyPlacement, error) {
	if raw == nil {
		return nil, nil
	}
	value := map[string]json.RawMessage{}
	kind := ""
	if json.Unmarshal(raw, &value) != nil || json.Unmarshal(value["type"], &kind) != nil {
		return nil, errors.New("position is invalid")
	}
	switch kind {
	case "page_start":
		return &pageBodyPlacement{Type: "start"}, nil
	case "page_end":
		return &pageBodyPlacement{Type: "end"}, nil
	case "after_block":
		after := struct {
			ID string `json:"id"`
		}{}
		if json.Unmarshal(value["after_block"], &after) != nil || strings.TrimSpace(after.ID) == "" {
			return nil, errors.New("position.after_block.id is required")
		}
		if pageID, err := resolveNotionCompatiblePageID(after.ID); err == nil {
			return &pageBodyPlacement{Type: "after", AfterKind: pageBodyItemPage, AfterID: pageID}, nil
		}
		blockID, ok := normalizeNotionUUIDRequest(after.ID)
		if !ok {
			return nil, errors.New("position.after_block.id is invalid")
		}
		return &pageBodyPlacement{Type: "after", AfterKind: pageBodyItemBlock, AfterID: blockID}, nil
	default:
		return nil, errors.New("position.type is invalid")
	}
}

func createNotionPageWithContent(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	parentID string,
	title string,
	titleSpans []graphTextSpan,
	presentation notionPagePresentationRequest,
	isLocked *bool,
	plan notionPageCreateContentPlan,
) (string, error) {
	pageID := newPageID()
	createRequestID := "notion-public-create:" + newDiagnosticReference()
	presentationMutationID := "notion-public-presentation:" + newDiagnosticReference()
	input := graphMutationInput{
		MutationID: "notion-public-create-content:" + newDiagnosticReference(),
		Operations: plan.Operations, TextUpdates: plan.TextUpdates,
	}
	hooks := contentGraphMutationHooks{
		BeforeApplyOperations: notionBlockReferencesBeforeApply(identity),
		// A new database's Verification comes with its Owner, as a data
		// source's does (database_verification_properties.go).
		LinkVerificationProperties: plan.Kind == pageKindDatabase,
		Authorize: func(txApp core.App) (pageAuthorization, error) {
			created, status, err := admitPageCreationTransactionWithLock(
				txApp, identity.PrincipalID, pageCreationAdmissionInput{
					RequestID: createRequestID, WorkspaceID: identity.WorkspaceID,
					ParentID: parentID, Title: title, TitleSpans: titleSpans, Content: plan.AdmissionContent, ContentInBody: true,
					PreparedPageID: pageID, BodyPlacement: plan.BodyPlacement, Kind: plan.Kind,
				}, pageLockIgnored,
			)
			if err == nil {
				err = notionPageCreationRefused(status)
			}
			if err != nil {
				return pageAuthorization{}, err
			}
			authorization, err := authorizePage(
				txApp, identity.PrincipalID, pageID, permissionWorkspaceWrite,
			)
			if err != nil {
				return pageAuthorization{}, err
			}
			if authorization.Workspace.WorkspaceID != identity.WorkspaceID ||
				!notionOAuthIdentityAllowsPage(txApp, identity, authorization) {
				return pageAuthorization{}, graphNotFoundError()
			}
			if created.Duplicate || presentation.Icon == nil && presentation.Cover == nil && isLocked == nil {
				return authorization, nil
			}
			if err := validateNotionPagePresentationReferences(
				txApp, identity.PrincipalID, identity.WorkspaceID, &presentation,
			); err != nil {
				return pageAuthorization{}, err
			}
			metadata := pageMetadataInput{
				Title: title, ParentID: parentID, InTrash: false,
				Icon: presentation.Icon, IconType: presentation.IconType, Cover: presentation.Cover,
				IsLocked:   isLocked,
				MutationID: presentationMutationID, BaseCursor: authorization.Page.GetInt("cursor"),
			}
			_, metadataStatus, err := applyPageMetadataTransaction(
				txApp, identity.PrincipalID, pageID, metadata,
				pageMetadataRequestHash(pageID, metadata),
			)
			if err != nil || metadataStatus != pageMetadataApplied {
				if err != nil {
					return pageAuthorization{}, err
				}
				return pageAuthorization{}, graphConflictError(
					errorCodeGraphPageCursorConflict, "The page presentation could not be applied.",
				)
			}
			return authorizePage(txApp, identity.PrincipalID, pageID, permissionWorkspaceWrite)
		},
		PrepareInput: func(
			txApp core.App,
			authorization pageAuthorization,
			doc *crdt.Doc,
			current graphMutationInput,
		) (graphMutationInput, error) {
			files := notionMarkdownFiles{
				WorkspaceID: authorization.Workspace.WorkspaceID,
				Upload: func(app core.App, _, uploadID string) (string, error) {
					content, err := resolveNotionCompletedFileUpload(app, identity, uploadID)
					return content.FileID, err
				},
			}
			if err := files.place(txApp, current.Operations); err != nil {
				return graphMutationInput{}, err
			}
			if err := resolveNotionMarkdownCalloutIcons(
				txApp, authorization.Workspace.WorkspaceID, current.Operations,
			); err != nil {
				return graphMutationInput{}, err
			}
			var err error
			current, err = prepareNotionMentionsInput(txApp, identity, authorization, current, plan.Markdown)
			if err != nil {
				return graphMutationInput{}, err
			}
			if len(current.TextUpdates) == 0 {
				return current, nil
			}
			graphRevision, err := currentNotionGraphRevision(
				txApp, authorization.Workspace.OwnerID, pageID,
			)
			if err != nil {
				return graphMutationInput{}, err
			}
			pageCursor := authorization.Page.GetInt("cursor")
			pageTitle := authorization.Page.GetString("title")
			current.BaseGraphRevision = &graphRevision
			current.BasePageCursor = &pageCursor
			current.PageTitle = &pageTitle
			return current, nil
		},
	}
	if _, err := ygo.applyContentGraphMutationContext(
		event.Request.Context(), identity.PrincipalID, pageID, input, hooks,
	); err != nil {
		return "", err
	}
	return pageID, nil
}

// notionPageCreateContentRefused says why a new page's content is refused for
// its faults (ContentRefused) as Notion's API says it, once the page's
// values, icon and cover have passed (seen 2026-11-10, a page and a
// database's page alike): an upload of its blocks it can't take, then a
// link's or a synced block's target it can't reach, then the faults
// together.
func notionPageCreateContentRefused(
	app core.App,
	identity notionOAuthBearerIdentity,
	scope pageAuthorization,
	plan notionPageCreateContentPlan,
) error {
	if _, err := prepareNotionBlockFileUploadsInput(
		app, identity, graphMutationInput{Operations: copiedGraphOperations(plan.Operations)},
	); err != nil {
		return err
	}
	if refused := notionRequestTargetRefused(app, identity, scope, plan.Operations); refused != nil {
		return refused
	}
	return graphInputError(errorCodeGraphInvalid, plan.ContentRefused.Error())
}

// notionPageCreateRefusal is a new page's refusal, in Leafen's words, where
// Notion's API refuses one: one
// placed after a block not at the top of its parent's body — another page's,
// one within a toggle, or the parent itself (seen 2026-11-14).
func notionPageCreateRefusal(message string) string {
	if message == pagePositionReferenceGone {
		return "The block given isn't inside the parent page."
	}
	return message
}

// notionPageCreateContentError answers a page write's refusal — a page made,
// moved, taken out of the trash or given a template — as Notion's API says
// it: one not found, refused or met by another change as such, never as a
// request it couldn't complete.
func notionPageCreateContentError(event *core.RequestEvent, err error) error {
	requestError := &graphRequestError{}
	if errors.As(err, &requestError) {
		switch requestError.status {
		case http.StatusNotFound:
			return notionPageNotFound(event)
		case http.StatusBadRequest:
			return notionPageValidationError(event, notionPageCreateRefusal(requestError.message))
		case http.StatusConflict:
			return notionGraphConflictError(event, requestError)
		}
	}
	if isDLPPolicyDecisionError(err) {
		return notionDLPPolicyError(event, err)
	}
	return notionRequestNotCompleted(event, err)
}
