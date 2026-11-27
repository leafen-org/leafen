package app

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database update's text: the description its first source keeps, and a
// linked database's own title and icon, kept on its block.

// notionDatabaseDescription reads a requested description as the text and
// formatting a database's "설명" keeps, as Notion's takes rich text (seen
// 2026-10-30); empty rich text clears it. mentions checks and names its
// mentions, as the name's are (notionNameMentions); a mention Notion refuses
// for what it names is said alone.
func notionDatabaseDescription(raw json.RawMessage, mentions func([]graphTextSpan) error) (string, []graphTextSpan, error) {
	if emptyNotionRichText(raw) {
		return "", nil, nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "", nil, notionDatabaseRowPageRequestInvalid("The database description is invalid.")
	}
	spans, err := notionTextRequestGraphSpans(value)
	if alone, refused := notionMentionRefusedAlone(err); refused {
		return "", nil, alone
	}
	if err != nil {
		return "", nil, notionDatabaseRowPageRequestInvalid("The database description is invalid.")
	}
	if err := mentions(spans); err != nil {
		alone, _ := notionMentionRefusedAlone(err)
		return "", nil, alone
	}
	var text strings.Builder
	for _, span := range spans {
		text.WriteString(span.Text)
	}
	if utf8.RuneCountInString(text.String()) > maxGraphTextCharacters {
		return "", nil, notionDatabaseRowPageRequestInvalid("The database description is longer than %d characters.", maxGraphTextCharacters)
	}
	encoded, _ := json.Marshal(text.String())
	kept, err := normalizeGraphRowValueSpans(graphValueTypeRichText, encoded, spans)
	if err != nil {
		return "", nil, notionDatabaseRowPageRequestInvalid("The database description's formatting is invalid.")
	}
	return text.String(), kept, nil
}

// updateNotionLinkedDatabaseName writes a linked database's own title, with
// its formatting, and icon to its block, as the editor names it; an empty
// title or no icon goes back to its source's. A full-page one's name and
// icon are its page's (database_turn_linked_name.go), which the update
// writes after this: its block keeps only the name's formatting
// (database_page_name_spans.go).
func updateNotionLinkedDatabaseName(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, input notionDatabaseUpdateRequest, title string, titleSpans []graphTextSpan,
) error {
	if container.fullPage() {
		if input.Title == nil {
			return nil
		}
		return updateNotionDatabasePageNameSpans(event, ygo, identity, container, titleSpans)
	}
	icon, iconType := "", graphDataSourceIconNone
	if input.Icon != nil {
		glyph, kind, err := notionDataSourceIcon(event.App, identity, input.Icon)
		if err != nil {
			return err
		}
		if glyph != nil && kind != nil {
			icon, iconType = *glyph, *kind
		}
	}
	return updateNotionDatabaseBlockContent(event, ygo, identity, container, "notion-public-database-name:", func(content map[string]any) {
		if input.Title != nil {
			delete(content, "linkedTitle")
			delete(content, "linkedTitleSpans")
			if title != "" {
				content["linkedTitle"] = title
				if len(titleSpans) > 0 {
					content["linkedTitleSpans"] = titleSpans
				}
			}
		}
		if input.Icon != nil {
			delete(content, "linkedIcon")
			delete(content, "linkedIconType")
			if iconType != graphDataSourceIconNone {
				content["linkedIcon"], content["linkedIconType"] = icon, iconType
			}
		}
	})
}

// updateNotionDatabasePageNameSpans keeps the formatting of a full-page
// database's name that its page's title names, of several sources of its own
// or a linked one, on its block (database_page_name_spans.go); none takes it
// away.
func updateNotionDatabasePageNameSpans(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, titleSpans []graphTextSpan,
) error {
	return updateNotionDatabaseBlockContent(event, ygo, identity, container, "notion-public-database-page-name:", databasePageNameSpansChange(titleSpans))
}

// databasePageNameSpansChange is a database block's content change that
// keeps a name's spans on it as pageTitleSpans, none taking them away.
func databasePageNameSpansChange(titleSpans []graphTextSpan) func(content map[string]any) {
	return func(content map[string]any) {
		delete(content, "pageTitleSpans")
		if len(titleSpans) > 0 {
			content["pageTitleSpans"] = titleSpans
		}
	}
}

// updateNotionDatabaseBlockContent changes a database's block content as the
// editor does.
func updateNotionDatabaseBlockContent(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, mutation string, change func(content map[string]any),
) error {
	_, err := ygo.applyContentGraphMutationContext(
		event.Request.Context(), identity.PrincipalID, container.PageID,
		graphMutationInput{MutationID: mutation + newDiagnosticReference()},
		contentGraphMutationHooks{
			Authorize: authorizeNotionPageMutation(identity, container.PageID),
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				return databaseBlockContentChange(txApp, doc, container.Owner, container.PageID, container.BlockID, change)
			},
		},
	)
	return err
}

// databaseBlockContentChange is the change that writes a database's block
// content changed as given, where it stands.
func databaseBlockContentChange(
	txApp core.App, doc *crdt.Doc, owner, pageID, blockID string, change func(content map[string]any),
) (contentGraphMutationPreparation, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return contentGraphMutationPreparation{}, err
	}
	entry, exists := metadata[blockID]
	if !exists || entry.Type != graphBlockTypeInlineDatabase {
		return contentGraphMutationPreparation{}, errDatabaseRowPageNotFound
	}
	var position string
	if err := txApp.DB().NewQuery(`
		SELECT [[position]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": blockID}).Row(&position); err != nil {
		return contentGraphMutationPreparation{}, err
	}
	content := make(map[string]any, len(entry.Content)+3)
	for key, value := range entry.Content {
		content[key] = value
	}
	change(content)
	encoded, err := json.Marshal(content)
	if err != nil {
		return contentGraphMutationPreparation{}, err
	}
	block := &graphBlock{ID: blockID, Position: position, Type: graphBlockTypeInlineDatabase, Content: encoded}
	if entry.ParentID != "" {
		parentID := entry.ParentID
		block.ParentID = &parentID
	}
	return contentGraphMutationPreparation{Operations: []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: block}}}, nil
}
