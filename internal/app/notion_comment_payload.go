package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionCommentDefaultPageSize = 100
	notionCommentMaximumPageSize = 100
	notionCommentMaximumFiles    = 3
)

type notionCommentParentRequest struct {
	Type    string `json:"type,omitempty"`
	PageID  string `json:"page_id,omitempty"`
	BlockID string `json:"block_id,omitempty"`
}

type notionCommentAttachmentRequest struct {
	Type         string `json:"type,omitempty"`
	FileUploadID string `json:"file_upload_id"`
}

type notionCommentDisplayNameRequest struct {
	Type   string `json:"type"`
	Custom *struct {
		Name string `json:"name"`
	} `json:"custom,omitempty"`
}

type notionCommentCreateRequest struct {
	Parent       *notionCommentParentRequest      `json:"parent,omitempty"`
	DiscussionID string                           `json:"discussion_id,omitempty"`
	RichText     json.RawMessage                  `json:"rich_text,omitempty"`
	Markdown     *string                          `json:"markdown,omitempty"`
	Attachments  []notionCommentAttachmentRequest `json:"attachments,omitempty"`
	DisplayName  *notionCommentDisplayNameRequest `json:"display_name,omitempty"`
	// MarkdownVersion is the version Markdown is written in
	// (notionCommentMarkdown).
	MarkdownVersion string `json:"markdown_version,omitempty"`
}

type notionCommentUpdateRequest struct {
	RichText        json.RawMessage `json:"rich_text,omitempty"`
	Markdown        *string         `json:"markdown,omitempty"`
	MarkdownVersion string          `json:"markdown_version,omitempty"`
}

// notionStoredCommentMetadata is what a connection's comment keeps of how
// it was written. The app shows the comment by it, as Notion's screen does
// (seen 2026-11-12): an integration's or a custom name on the connection's
// face, a custom one with the connection named beside it (ConnectionName).
type notionStoredCommentMetadata struct {
	CreatorKey      string `json:"creatorKey"`
	DisplayNameType string `json:"displayNameType"`
	ResolvedName    string `json:"resolvedName"`
	ConnectionName  string `json:"connectionName,omitempty"`
}

type notionStoredCommentBody struct {
	Type          string                       `json:"type"`
	Rich          []mcpCommentSpan             `json:"rich"`
	Quote         string                       `json:"quote,omitempty"`
	AttachmentIDs []string                     `json:"attachmentIds,omitempty"`
	Notion        *notionStoredCommentMetadata `json:"notion,omitempty"`
}

func notionCommentCreatorKey(identity notionOAuthBearerIdentity) string {
	if identity.ClientID == "personal-access-token" {
		return "pat:" + identity.GrantID
	}
	return "oauth:" + identity.ClientID
}

func decodeNotionStoredCommentBody(raw string) (notionStoredCommentBody, error) {
	body := notionStoredCommentBody{}
	// A comment of no words holds an empty list (notionCommentGraphSpans).
	if json.Unmarshal([]byte(raw), &body) != nil || body.Type != "rich_text" ||
		body.Rich == nil || len(body.Rich) > mcpCommentMaximumRichTextItems {
		return notionStoredCommentBody{}, errCommentFailed
	}
	return body, nil
}

func decodeNotionCommentProjectionBody(raw string) (notionStoredCommentBody, error) {
	body := notionStoredCommentBody{}
	if json.Unmarshal([]byte(raw), &body) == nil &&
		body.Rich != nil && len(body.Rich) <= mcpCommentMaximumRichTextItems {
		return body, nil
	}
	var legacy any
	if json.Unmarshal([]byte(raw), &legacy) != nil {
		return notionStoredCommentBody{}, errCommentFailed
	}
	text := ""
	switch value := legacy.(type) {
	case string:
		text = value
	case map[string]any:
		text, _ = value["text"].(string)
	}
	if text == "" {
		return notionStoredCommentBody{Type: "rich_text", Rich: []mcpCommentSpan{}}, nil
	}
	if utf8.RuneCountInString(text) > mcpCommentMaximumTextRunes {
		return notionStoredCommentBody{}, errCommentFailed
	}
	return notionStoredCommentBody{
		Type: "rich_text", Rich: []mcpCommentSpan{{Text: text}},
	}, nil
}

// encodeNotionStoredCommentBody writes a body with "<", ">" and "&" as
// they are, so whatever a request holds fits a comment's bound
// (comment-wire's bodyBytes).
func encodeNotionStoredCommentBody(body notionStoredCommentBody) (json.RawMessage, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(body); err != nil {
		return nil, errCommentInvalid
	}
	encoded := json.RawMessage(bytes.TrimRight(out.Bytes(), "\n"))
	if !validCommentBody(encoded) {
		return nil, errCommentInvalid
	}
	return encoded, nil
}

// notionCommentDisplayName is how a new comment shows its writer, as its
// request asks, which its shape has checked (notionCommentCreateShape): the
// integration's name, its person's, or one of its own as written, of any
// length, with the connection's name beside it. An internal connection has
// no one person to show (seen 2026-11-11).
func notionCommentDisplayName(
	app core.App,
	identity notionOAuthBearerIdentity,
	request *notionCommentDisplayNameRequest,
) (notionStoredCommentMetadata, error) {
	typeName := "integration"
	if request != nil {
		typeName = request.Type
	}
	shown := notionStoredCommentMetadata{
		CreatorKey: notionCommentCreatorKey(identity), DisplayNameType: typeName,
	}
	switch typeName {
	case "user":
		if notionInternalConnection(identity) {
			return shown, notionInvalid(errCommentInvalid, `An internal integration belongs to no one person, so display_name type "user" isn't open to it: use "integration" or "custom".`)
		}
		user, err := app.FindRecordById("users", identity.PrincipalID)
		if err != nil {
			return shown, err
		}
		shown.ResolvedName = strings.TrimSpace(user.GetString("name"))
		return shown, nil
	case "custom":
		if request.Custom == nil || request.Custom.Name == "" {
			return shown, notionInvalid(errCommentInvalid, "display_name type custom needs custom.name.")
		}
	}
	connection, err := notionCommentConnectionName(app, identity)
	if err != nil {
		return shown, err
	}
	shown.ResolvedName, shown.ConnectionName = connection, connection
	if typeName == "custom" {
		shown.ResolvedName = request.Custom.Name
	}
	return shown, nil
}

// notionCommentConnectionName is the name of the connection writing: its
// personal access token's, or its integration's.
func notionCommentConnectionName(app core.App, identity notionOAuthBearerIdentity) (string, error) {
	name := "Leafen integration"
	if identity.ClientID == "personal-access-token" {
		if err := app.DB().NewQuery(`
			SELECT [[name]] FROM {{leafen_notion_personal_access_tokens}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId}
		`).Bind(dbx.Params{"id": identity.GrantID, "workspaceId": identity.WorkspaceID}).Row(&name); err != nil {
			return "", err
		}
	} else {
		if err := app.DB().NewQuery(`
			SELECT [[client_name]] FROM {{leafen_notion_oauth_grants}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId}
		`).Bind(dbx.Params{"id": identity.GrantID, "workspaceId": identity.WorkspaceID}).Row(&name); err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(name), nil
}

func notionCommentRichTextSpans(
	app core.App,
	identity notionOAuthBearerIdentity,
	page pageAuthorization,
	raw json.RawMessage,
) ([]mcpCommentSpan, error) {
	items := []json.RawMessage{}
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
		return nil, notionInvalid(errCommentInvalid, "rich_text takes a list of rich text objects.")
	}
	spans, err := notionCommentGraphSpansHere(app, page, items)
	if err != nil {
		return nil, err
	}
	result := make([]mcpCommentSpan, 0, len(spans))
	for _, span := range spans {
		item := mcpCommentSpan{
			Text: span.Text, Bold: span.Bold, Italic: span.Italic,
			Underline: span.Underline, Strikethrough: span.Strikethrough,
			Code: span.Code, Color: notionCommentSpanColor(span), Link: span.Link,
			Mention: span.Mention, PageID: span.PageID, DatabaseID: span.DatabaseID,
			CustomEmojiID: span.CustomEmojiID, UserID: span.UserID,
			Date: span.Date, DateEnd: span.DateEnd, DateTimeZone: span.DateTimeZone,
			Equation: span.Equation,
		}
		if err := authorizeNotionCommentSpan(app, identity, page, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func authorizeNotionCommentSpan(
	app core.App,
	identity notionOAuthBearerIdentity,
	page pageAuthorization,
	span *mcpCommentSpan,
) error {
	switch span.Mention {
	case "":
		return nil
	case "person":
		user, found, err := loadNotionMentionUser(app, identity.WorkspaceID, span.UserID)
		if err != nil {
			return err
		}
		if found {
			// Kept as "@" and their name, as a block's mention is, not the ID
			// it was written with (2026-11-12: a notice's line said the ID).
			span.Text = notionPersonMentionWords(user.Name, span.UserID)
			return nil
		}
		// A bot of the workspace's connections, or the writing token's own,
		// is one, kept by its ID and named as it is, as a block's text keeps
		// one (seen 2026-11-11 in a comment too); anyone else is a person
		// Notion can't find.
		if bot, found := notionMentionBotFor(app, identity, identity.WorkspaceID, span.UserID); found {
			span.UserID, span.Text = bot.ID, "@"+bot.Name
			return nil
		}
		publicID, err := notionCompatibleUserID(span.UserID)
		if notionBotUserIDShape(span.UserID) || err != nil {
			publicID = span.UserID
		}
		return &notionMentionRefusal{missingUser: publicID}
	case "page":
		// One written with a block's ID names the block's page at the
		// block, by its words, as a block's text does
		// (page_mention_block_anchor.go).
		if block, found, err := pageMentionBlock(app, page.Workspace.WorkspaceID, span.PageID); err != nil || found {
			if err != nil {
				return err
			}
			ownerID := page.Workspace.OwnerID
			_, allowed, err := authorizeNotionPageMentionTarget(app, identity, ownerID, identity.WorkspaceID, block.PageID)
			if err != nil {
				return err
			}
			span.PageID, span.BlockID, span.Text = block.PageID, block.ID, ""
			if allowed {
				span.Text = notionBlockMentionWords(app, identity, ownerID, identity.WorkspaceID, block)
			}
			if span.Text == "" {
				span.Text = notionUnresolvedMentionTitle
			}
			return nil
		}
		// A page the connection can't reach, or that nothing answers to, is
		// kept, as Notion's API keeps such a mention in a block's text (seen
		// 2026-11-09) and its MCP in a comment: the answer says nothing of
		// which it was, and reads it "Untitled" (mcp_reader_mentions.go). One
		// of the workspace keeps its title, as a block's does
		// (notionMentionPreparation.pageMention), for whoever may open it.
		pageID, resolved := resolveNotionPageMentionID(app, span.PageID)
		if !resolved {
			span.PageID = strings.TrimPrefix(span.PageID, notionPageMentionRowPrefix)
			span.Text = notionUnresolvedMentionTitle
			return nil
		}
		target, allowed, err := authorizeNotionPageMentionTarget(
			app, identity, page.Workspace.OwnerID, identity.WorkspaceID, pageID,
		)
		if err != nil {
			return err
		}
		span.PageID = pageID
		if !allowed {
			span.Text = notionUnresolvedMentionTitle
			if now, workspaceID, there := pageMentionTargetNow(app, pageID); there && workspaceID == identity.WorkspaceID && now != "" {
				span.Text = now
			}
			return nil
		}
		// A mention written without a name, as Notion writes one, takes the
		// page's, or Notion's API's name for a page with none ("New page").
		if _, unnamed := normalizeNotionUUIDRequest(span.Text); unnamed {
			if span.Text = target.Title; span.Text == "" {
				span.Text = notionUntitledPageMentionTitle
			}
		}
	case "database":
		// A database named by its public ID is its own, as a block's
		// mention reads it (notionMentionPreparation.databaseContainerID).
		containerID, found, err := (&notionMentionPreparation{app: app, current: page}).databaseContainerID(span.DatabaseID)
		if err != nil {
			return err
		}
		title, allowed := "", false
		if found {
			span.DatabaseID = containerID
			if title, allowed, err = notionDatabaseMentionTitle(app, identity, page, containerID); err != nil {
				return err
			}
			// One the connection can't reach keeps its name, as a page's does.
			if !allowed {
				if title, _, err = databaseContainerTitle(app, containerID); err != nil {
					return err
				}
				span.Text = title
			}
		}
		// A mention written without a name, as Notion writes one, takes the
		// database's, "New database" for one without, as a block's does.
		if _, unnamed := normalizeNotionUUIDRequest(span.Text); unnamed && allowed {
			if span.Text = title; strings.TrimSpace(title) == "" {
				span.Text = notionUntitledDatabaseMentionTitle
			}
		}
		if strings.TrimSpace(title) == "" && !allowed {
			span.Text = notionUnresolvedMentionTitle
		}
	case "custom_emoji":
		emojiID := strings.ToLower(span.CustomEmojiID)
		emoji, allowed, err := loadNotionCustomEmojiMention(
			app, identity.PrincipalID, identity.WorkspaceID, emojiID,
		)
		if err != nil {
			return err
		}
		// One the workspace hasn't is kept by its ID as written, as a
		// block's text keeps it: Notion takes it in a comment and reads it
		// back as its <unknown_mention> (seen 2026-11-11 through Notion's
		// MCP; notion_missing_custom_emoji.go).
		if !allowed {
			span.Text = notionMissingCustomEmojiText
			return nil
		}
		span.CustomEmojiID, span.Text = emojiID, ":"+strings.TrimSpace(emoji.Name)+":"
	case "date":
		return nil
	default:
		return notionInvalid(errCommentInvalid, "mention.type takes user, page, database, date or custom_emoji.")
	}
	return nil
}

func notionCommentBodyFromRequest(
	app core.App,
	identity notionOAuthBearerIdentity,
	page pageAuthorization,
	richText json.RawMessage,
	markdown *notionCommentMarkdown,
	attachmentIDs []string,
	metadata *notionStoredCommentMetadata,
) (json.RawMessage, error) {
	if (len(richText) == 0) == (markdown == nil) {
		return nil, notionInvalid(errCommentInvalid, "Give rich_text or markdown, one of them.")
	}
	var spans []mcpCommentSpan
	var err error
	if markdown != nil {
		spans, err = notionCommentMarkdownSpans(app, identity, page, *markdown)
	} else {
		spans, err = notionCommentRichTextSpans(app, identity, page, richText)
	}
	if err != nil {
		return nil, err
	}
	return encodeNotionStoredCommentBody(notionStoredCommentBody{
		Type: "rich_text", Rich: spans, AttachmentIDs: attachmentIDs, Notion: metadata,
	})
}

func notionCommentMetadataOwnedBy(body notionStoredCommentBody, identity notionOAuthBearerIdentity) bool {
	return body.Notion != nil && body.Notion.CreatorKey == notionCommentCreatorKey(identity)
}

// notionComment is a comment as Notion's API answers it, its keys in
// Notion's order (seen 2026-11-11): attachments only when it has some.
type notionComment struct {
	Object                 string                  `json:"object"`
	ID                     string                  `json:"id"`
	Parent                 notionCommentParent     `json:"parent"`
	DiscussionID           string                  `json:"discussion_id"`
	CreatedTime            string                  `json:"created_time"`
	LastEditedTime         string                  `json:"last_edited_time"`
	CreatedBy              notionCommentUser       `json:"created_by"`
	RichText               []map[string]any        `json:"rich_text"`
	DisplayName            notionCommentName       `json:"display_name"`
	OriginalContentDeleted bool                    `json:"original_content_deleted"`
	HasPendingAttachments  bool                    `json:"has_pending_attachments"`
	Attachments            []notionCommentAttached `json:"attachments,omitempty"`
}

type notionCommentParent struct {
	Type    string `json:"type"`
	PageID  string `json:"page_id,omitempty"`
	BlockID string `json:"block_id,omitempty"`
}

type notionCommentUser struct {
	Object string `json:"object"`
	ID     string `json:"id"`
}

type notionCommentName struct {
	Type         string `json:"type"`
	ResolvedName string `json:"resolved_name"`
}

type notionCommentAttached struct {
	Category string                 `json:"category"`
	File     notionCommentFileValue `json:"file"`
}

type notionCommentFileValue struct {
	URL        string `json:"url"`
	ExpiryTime any    `json:"expiry_time"`
}

// notionCommentObject is a comment as a connection reads it: a mention of a
// page it can't reach reads "Untitled" (notionIdentityCloses); originals
// reads whether its discussion lost what it was left on
// (notion_comment_original_content.go), for every comment of one answer.
func notionCommentObject(
	app core.App,
	row storedComment,
	issuer *notionBlockFileCapabilityIssuer,
	identity notionOAuthBearerIdentity,
	originals *notionCommentOriginals,
) (notionComment, error) {
	publicID, err := notionObjectIdentityPublicID(
		app, notionObjectIdentityComment, row.WorkspaceID, row.ID,
	)
	if err != nil {
		return notionComment{}, err
	}
	publicAuthorID, _, err := connectionAuthorIDs(app, connectionAuthoredComment, row.ID, "", row.AuthorID, row.AuthorID)
	if err != nil {
		return notionComment{}, err
	}
	body, err := decodeNotionCommentProjectionBody(row.BodyJSON)
	if err != nil {
		return notionComment{}, err
	}
	richText := make([]map[string]any, 0, len(body.Rich))
	closed := notionIdentityCloses(app, identity)
	for _, span := range body.Rich {
		// What a block's text leaves out is left out (notionCommentSpanLeftOut).
		if leftOut, err := notionCommentSpanLeftOut(app, identity.PrincipalID, row.WorkspaceID, span); err != nil {
			return notionComment{}, err
		} else if leftOut {
			continue
		}
		item, _, err := notionMCPRichTextFromSpan(app, row.WorkspaceID, span, closed, issuer, identity)
		if err != nil {
			return notionComment{}, err
		}
		richText = append(richText, notionRichTextPieces(item)...)
	}
	parent := notionCommentParent{Type: "page_id"}
	if row.BlockID != "" {
		// A database page's body blocks are page blocks, named as the block
		// API names them, a row block from before by its own name — a
		// deleted one's too, which its discussion is still on.
		blockID, err := notionBlockPublicID(app, row.WorkspaceID, row.BlockID)
		if err != nil {
			return notionComment{}, err
		}
		parent = notionCommentParent{Type: "block_id", BlockID: blockID}
	} else {
		if row.TargetKind == commentTargetDatabaseRow {
			parent.PageID, err = notionObjectIdentityPublicID(
				app, notionObjectIdentityRowPage, row.WorkspaceID, row.TargetID,
			)
		} else {
			parent.PageID, err = notionPagePublicID(app, row.PageID)
		}
		if err != nil {
			return notionComment{}, err
		}
	}
	displayType, resolvedName := "user", ""
	if body.Notion != nil {
		displayType, resolvedName = body.Notion.DisplayNameType, body.Notion.ResolvedName
	}
	if resolvedName == "" {
		user, findErr := app.FindRecordById("users", row.AuthorID)
		if findErr != nil {
			return notionComment{}, findErr
		}
		resolvedName = strings.TrimSpace(user.GetString("name"))
	}
	response := notionComment{
		Object: "comment", ID: publicID, Parent: parent,
		DiscussionID: row.NotionDiscussionID,
		CreatedTime:  row.CreatedAt, LastEditedTime: row.UpdatedAt,
		CreatedBy:   notionCommentUser{Object: "user", ID: publicAuthorID},
		RichText:    richText,
		DisplayName: notionCommentName{Type: displayType, ResolvedName: resolvedName},
	}
	if response.OriginalContentDeleted, err = originals.deleted(app, row); err != nil {
		return notionComment{}, err
	}
	files, pending, err := notionCommentAttachmentsNow(app, row.WorkspaceID, body.AttachmentIDs)
	if err != nil {
		return notionComment{}, failedBecause(errCommentFailed, err)
	}
	response.HasPendingAttachments = pending
	// The same file attached twice is listed twice, as Notion lists it; one
	// still uploading, or gone, is left out.
	for _, fileID := range body.AttachmentIDs {
		file, whole := files[fileID]
		if !whole {
			continue
		}
		url, expiry, err := issuer.managedFileCapability(app, row.WorkspaceID, fileID)
		if err != nil {
			return notionComment{}, err
		}
		response.Attachments = append(response.Attachments, notionCommentAttached{
			Category: notionCommentAttachmentCategory(file.ContentType),
			File:     notionCommentFileValue{URL: url, ExpiryTime: expiry},
		})
	}
	return response, nil
}

func notionCommentAttachmentCategory(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "audio/"):
		return "audio"
	case strings.HasPrefix(contentType, "image/"):
		return "image"
	case contentType == "application/pdf":
		return "pdf"
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	default:
		return "productivity"
	}
}
