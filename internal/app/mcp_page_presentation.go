package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's MCP page tools take a page's icon and cover as text: an icon is
// an emoji, a custom emoji by :name:, a Notion icon identifier as its fetch
// gives it (icons/pizza_blue), or an image URL, and a cover is an image URL;
// "none" takes either away, and leaving it out keeps it. They then read as
// the Public API's icon and cover objects (notionPagePresentation), a custom
// emoji by its name in the page's workspace. As Notion's tools describe it
// (developers.notion.com's MCP reference and the tools' own words, read
// 2026-11-26), a cover is an external image URL and no uploaded file; a
// refusal says what a cover takes and the way an uploaded picture becomes one
// (mcpPageCoverRefused). Before (2026-11-26) it offered a file_upload_id it
// then refused, and notion-create-pages answered any icon or cover it refused
// with "an emoji or an image address", leaving out what else an icon takes.

// mcpPagePresentation reads an MCP page tool's icon and cover; set is false
// when the request gives neither.
func mcpPagePresentation(
	app core.App, workspaceID string, icon, cover *string,
) (presentation notionPagePresentationRequest, set bool, err error) {
	if icon == nil && cover == nil {
		return notionPagePresentationRequest{}, false, nil
	}
	var rawIcon, rawCover json.RawMessage
	if icon != nil {
		if rawIcon, err = mcpPageIconObject(app, workspaceID, strings.TrimSpace(*icon)); err != nil {
			return notionPagePresentationRequest{}, false, err
		}
	}
	if cover != nil {
		value := strings.TrimSpace(*cover)
		switch {
		case value == "none":
			rawCover = json.RawMessage("null")
		case notionPageExternalFile(value) != nil:
			rawCover, _ = json.Marshal(map[string]any{"type": "external", "external": map[string]any{"url": value}})
		default:
			return notionPagePresentationRequest{}, false, mcpPageCoverRefused()
		}
	}
	presentation, err = notionPagePresentation(rawIcon, rawCover)
	if err != nil {
		return notionPagePresentationRequest{}, false, mcpInvalid("icon is an emoji, an icons/… name, a custom emoji's :name: or an image address; cover an image address")
	}
	return presentation, true, nil
}

// mcpPageCoverRefused says what an MCP page cover takes, and how a picture
// uploaded with notion-create-file-upload becomes a cover: the page's own
// change of its look through leafen_call, which takes the file by its ID.
func mcpPageCoverRefused() error {
	return mcpInvalid("cover is an image address written from https:// or http:// on, or none to take the cover away; " +
		"an uploaded file isn't a cover here. A picture uploaded with notion-create-file-upload becomes the cover through leafen_call, " +
		"cover its file_upload_id: PATCH /pages/{id}/metadata for a page, PUT /database-rows/{rowId}/page/presentation for a database's page " +
		"(leafen_operations describes each)")
}

// notionMCPPageTitleProperty reads the property update_properties may set on
// an ordinary page: its title, as inline Markdown (mcpPageTitleMarkdown); null
// empties it. Nil means the request sets no title.
func notionMCPPageTitleProperty(properties map[string]any, sitePages *notionMarkdownSitePages) (*mcpWrittenTitle, error) {
	if len(properties) == 0 {
		return nil, nil
	}
	value, found := properties["title"]
	title, text := value.(string)
	if !found || len(properties) != 1 || value != nil && !text {
		return nil, fmt.Errorf("%w: an ordinary page takes only its title property, as text", errMCPToolInvalid)
	}
	read, ok := mcpPageTitleMarkdown(title, sitePages)
	if !ok {
		return nil, fmt.Errorf("%w: the title is too long", errMCPToolInvalid)
	}
	return &read, nil
}

// mcpWrittenTitle is an ordinary page's title as an MCP write gives it: its
// words and their spans, none for plain words, and what reading it said.
type mcpWrittenTitle struct {
	Words string
	Spans []graphTextSpan
	Said  []notionMarkdownWarning
}

// mcpPageTitleMarkdown reads a page's title written as an MCP write's inline
// Markdown (mcp_inline_markdown.go), its spaces at either end left out;
// false for words longer than a title holds.
func mcpPageTitleMarkdown(written string, sitePages *notionMarkdownSitePages) (mcpWrittenTitle, bool) {
	words, spans, said := mcpInlineMarkdownSpans(strings.TrimSpace(written), sitePages)
	return mcpWrittenTitle{Words: words, Spans: spans, Said: said}, utf8.RuneCountInString(words) <= maxGraphTitleRunes
}

// prepared is the title with its mentions named as the connection's
// Markdown names a page's (notionMarkdownPreparedTitle), in current's
// workspace.
func (title mcpWrittenTitle) prepared(app core.App, authority mcpAuthority, current pageAuthorization) (mcpWrittenTitle, error) {
	words, spans, err := notionMarkdownPreparedTitle(title.Words, title.Spans, true, func(spans []graphTextSpan) ([]graphTextSpan, error) {
		return mcpPreparedPropertySpans(app, authority, current, spans)
	})
	return mcpWrittenTitle{Words: words, Spans: spans, Said: title.Said}, err
}

// mcpPagePresentationObjects reads a page's icon and cover as Notion's MCP
// fetch gives them: the Public API's objects, null when the page has none, an
// uploaded image's by a link that expires.
func mcpPagePresentationObjects(
	app core.App, authority mcpAuthority, page *core.Record,
) (icon, cover *map[string]any, err error) {
	resource, err := url.Parse(authority.Resource)
	if err != nil || resource.Scheme != "http" && resource.Scheme != "https" || resource.Host == "" {
		return nil, nil, failedBecause(errMCPToolFailed, err)
	}
	issuer := &notionBlockFileCapabilityIssuer{
		origin: resource.Scheme + "://" + resource.Host, principalID: authority.PrincipalID, issuedAt: time.Now().UTC(),
	}
	workspaceID := page.GetString("workspace")
	iconValue, err := notionPageIconWithManagedFile(
		app, issuer, workspaceID, page.GetString("icon_type"), page.GetString("icon"),
	)
	if err != nil {
		return nil, nil, err
	}
	coverValue, err := notionPageCoverWithManagedFile(app, issuer, workspaceID, page.GetString("cover"))
	if err != nil {
		return nil, nil, err
	}
	object := func(value any) (*map[string]any, error) {
		result := map[string]any(nil)
		if value == nil {
			return &result, nil
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return &result, json.Unmarshal(encoded, &result)
	}
	if icon, err = object(iconValue); err != nil {
		return nil, nil, err
	}
	cover, err = object(coverValue)
	return icon, cover, err
}

// mcpPageIconObject is the Public API icon object an MCP icon text means.
func mcpPageIconObject(app core.App, workspaceID, value string) (json.RawMessage, error) {
	var object map[string]any
	switch {
	case value == "none":
		return json.RawMessage("null"), nil
	case len(value) > 2 && strings.HasPrefix(value, ":") && strings.HasSuffix(value, ":"):
		span := graphTextSpan{Text: value}
		if err := resolveNotionMarkdownCustomEmoji(app, workspaceID, &span); err != nil {
			return nil, err
		}
		if span.CustomEmojiID == "" {
			return nil, mcpInvalid("no custom emoji of that name in this workspace")
		}
		object = map[string]any{"type": "custom_emoji", "custom_emoji": map[string]any{"id": span.CustomEmojiID}}
	case strings.HasPrefix(value, "icons/"):
		if _, notion, err := nativeIconFromNotionIdentifier(value); !notion || err != nil {
			return nil, mcpInvalid("that icons/… name isn't one Leafen has")
		}
		// The identifier names the icon as its image URL does.
		url := "https://www.notion.so/" + value + ".svg"
		object = map[string]any{"type": "external", "external": map[string]any{"url": url}}
	case notionPageExternalFile(value) != nil:
		object = map[string]any{"type": "external", "external": map[string]any{"url": value}}
	case notionMarkdownCalloutEmoji(value):
		object = map[string]any{"type": "emoji", "emoji": value}
	default:
		return nil, mcpInvalid("icon is an emoji, an icons/… name, a custom emoji's :name: or an image address")
	}
	return json.Marshal(object)
}

// applyMCPPageMetadata sets a page's title, when one is given, and its icon
// and cover as the Public API's page update does.
func applyMCPPageMetadata(
	ctx context.Context, app core.App, principalID, workspaceID, pageID string,
	title *mcpWrittenTitle, presentation notionPagePresentationRequest,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		// The version it records names the connection, as every MCP write's does.
		defer namePageHistoryConnection(txApp, ctx)()
		if err := validateNotionPagePresentationReferences(txApp, principalID, workspaceID, &presentation); err != nil {
			return err
		}
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		metadata := pageMetadataInput{
			Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: page.GetBool("in_trash"),
			Icon: presentation.Icon, IconType: presentation.IconType, Cover: presentation.Cover,
			MutationID: "mcp-presentation:" + newDiagnosticReference(), BaseCursor: page.GetInt("cursor"),
		}
		if title != nil {
			// The title given is the whole title, its formatting too.
			spans := title.Spans
			if spans == nil {
				spans = []graphTextSpan{}
			}
			metadata.Title, metadata.TitleSpans = title.Words, &spans
		}
		_, status, err := applyPageMetadataTransaction(
			txApp, principalID, pageID, metadata, pageMetadataRequestHash(pageID, metadata),
		)
		if err != nil {
			return err
		}
		return mcpPageMetadataRefusal(status, "The page title, icon and cover could not be applied.")
	})
}

// mcpPageMetadataRefusal names why the page metadata writer left an MCP
// change, read from the page in the same transaction, unapplied: a page it
// can't write is missing (it took a database page as one, 2026-11-12), a
// body being replaced asks to wait, anything else is a conflict.
func mcpPageMetadataRefusal(status pageMetadataMutationStatus, conflict string) error {
	switch status {
	case pageMetadataApplied:
		return nil
	case pageMetadataPageMissing:
		return graphNotFoundError()
	case pageMetadataReplacementLocked:
		return mcpInvalid("the page's content is being replaced: try again when that is done")
	}
	return graphConflictError(errorCodeGraphPageCursorConflict, conflict)
}

// updateMCPPageProperties sets the properties, icon and cover an MCP page
// update gives the page it may write, and answers with the page and its body:
// an ordinary page's title, or a database page's values.
func updateMCPPageProperties(
	ctx context.Context, app core.App, authority mcpAuthority, pageID, mutationID string, input mcpUpdatePageInput,
	authorize func(core.App) (pageAuthorization, error),
) (*mcp.CallToolResult, mcpPageMutationOutput, error) {
	current, err := authorize(app)
	if err != nil {
		return nil, mcpPageMutationOutput{}, mcpPageMarkdownError(err)
	}
	// What reading their text says is told with the page (mcpInlineMarkdownSpans).
	row, said := isDatabaseRowPageRecord(current.Page), &mcpMarkdownSaid{}
	// A page its database's Verification property doesn't verify is
	// verified by its own, as its REST write does (page_verification_way.go).
	if input.Verification.given() {
		way, err := loadPageVerificationWay(app, pageID)
		if err != nil {
			return nil, mcpPageMutationOutput{}, mcpPageMarkdownError(err)
		}
		if !way.ByProperty {
			if err := verifyMCPPageItself(app, authority.PrincipalID, pageID, input.Verification); err != nil {
				return nil, mcpPageMutationOutput{}, err
			}
			input.Verification = mcpPageVerification{}
		}
	}
	if row {
		// A database page's properties, icon, cover and style go through
		// its row's writers, which take no cursor: the page metadata
		// writer opens it only to read (2026-11-12: its width was always
		// refused as a cursor conflict).
		if err := updateMCPDatabasePage(ctx, app, authority, current, input, said); err != nil {
			return nil, mcpPageMutationOutput{}, err
		}
	} else {
		title, err := notionMCPPageTitleProperty(input.Properties, mcpValueSitePages(app, authority, current))
		if err != nil {
			return nil, mcpPageMutationOutput{}, err
		}
		if title != nil {
			if *title, err = title.prepared(app, authority, current); err != nil {
				return nil, mcpPageMutationOutput{}, mcpPageMarkdownError(err)
			}
			said.add(title.Said)
		}
		presentation, _, err := mcpPagePresentation(app, current.Workspace.WorkspaceID, input.Icon, input.Cover)
		if err != nil {
			return nil, mcpPageMutationOutput{}, mcpReasoned(err)
		}
		// A verification alone writes no title, icon or cover.
		if title != nil || input.Icon != nil || input.Cover != nil {
			if err := applyMCPPageMetadata(
				ctx, app, authority.PrincipalID, current.Workspace.WorkspaceID, pageID, title, presentation,
			); err != nil {
				return nil, mcpPageMutationOutput{}, mcpPageMarkdownError(err)
			}
		}
		// A full-page database's one source follows its page's name and icon, as the app's change of them does.
		if err := nameFullPageDatabaseAfter(ctx, app, authority.PrincipalID, pageID, title != nil, input.Icon != nil); err != nil {
			return nil, mcpPageMutationOutput{}, mcpPageMarkdownError(err)
		}
	}
	if !row && (input.FullWidth != nil || input.SmallText != nil || input.Font != nil) {
		if err := applyMCPPageAppearance(ctx, app, authority.PrincipalID, pageID, input); err != nil {
			return nil, mcpPageMutationOutput{}, mcpPageMarkdownError(err)
		}
	}
	page, err := mcpPageResponse(app, pageID)
	if err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPPageUpdateFailed, err)
	}
	truncated := false
	if page.Content, truncated, err = mcpPageBodyMarkdownRead(app, mcpMarkdownReader(authority), current.Workspace.OwnerID, pageID); err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPPageUpdateFailed, err)
	}
	return nil, mcpPageMutationOutput{
		Page:      page,
		Receipt:   mutationReceiptResponse{MutationID: mutationID, Cursor: page.Cursor},
		Truncated: truncated, Warnings: said.answer(),
	}, nil
}

// applyMCPPageAppearance sets a page's width, text size and font, as its
// "..." menu does.
func applyMCPPageAppearance(ctx context.Context, app core.App, principalID, pageID string, input mcpUpdatePageInput) error {
	return app.RunInTransaction(func(txApp core.App) error {
		defer namePageHistoryConnection(txApp, ctx)()
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		metadata := pageMetadataInput{
			Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: page.GetBool("in_trash"),
			Font: input.Font, SmallText: input.SmallText, FullWidth: input.FullWidth,
			MutationID: "mcp-appearance:" + newDiagnosticReference(), BaseCursor: page.GetInt("cursor"),
		}
		_, status, err := applyPageMetadataTransaction(
			txApp, principalID, pageID, metadata, pageMetadataRequestHash(pageID, metadata),
		)
		if err != nil {
			return err
		}
		return mcpPageMetadataRefusal(status, "The page's appearance could not be changed in its current state.")
	})
}
