package app

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func prepareNotionRichTextMentions(
	identity notionOAuthBearerIdentity,
) func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error) {
	return func(
		app core.App,
		current pageAuthorization,
		_ *crdt.Doc,
		input graphMutationInput,
	) (graphMutationInput, error) {
		return prepareNotionRichTextMentionsInput(app, identity, current, input)
	}
}

func prepareNotionRichTextMentionsInput(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	input graphMutationInput,
) (graphMutationInput, error) {
	return prepareNotionMentionsInput(app, identity, current, input, false)
}

// prepareNotionMentionsInput points a write's links and mentions here
// (prepareNotionWriteTargetsInput), then checks and names its mentions: one
// its writer can't read, or of nothing, is kept, as Notion's keeps both (pageMention).
func prepareNotionMentionsInput(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	input graphMutationInput,
	markdown bool,
) (graphMutationInput, error) {
	var err error
	input, err = prepareNotionWriteTargetsInput(app, identity, current, input)
	if err != nil {
		return graphMutationInput{}, err
	}
	preparation := newNotionMentionPreparation(app, identity, current)
	preparation.markdown = markdown
	for updateIndex := range input.TextUpdates {
		if err := preparation.prepare(input.TextUpdates[updateIndex].Spans); err != nil {
			return graphMutationInput{}, err
		}
	}
	for operationIndex := range input.Operations {
		operation := &input.Operations[operationIndex]
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil {
			continue
		}
		if operation.Block.Type != graphBlockTypeTableRow {
			if operation.Block.Content, err = preparation.prepareCaption(operation.Block.Content); err != nil {
				return graphMutationInput{}, err
			}
			continue
		}
		content, err := parseGraphTableRowBlockContent(operation.Block.Content)
		if err != nil {
			return graphMutationInput{}, err
		}
		for cellIndex := range content.Cells {
			if err := preparation.prepare(content.Cells[cellIndex]); err != nil {
				return graphMutationInput{}, err
			}
		}
		operation.Block.Content, err = json.Marshal(content)
		if err != nil {
			return graphMutationInput{}, err
		}
	}
	return input, nil
}

type notionMentionPreparation struct {
	app              core.App
	identity         notionOAuthBearerIdentity
	current          pageAuthorization
	pageTitles       map[string]string
	databaseTitles   map[string]string
	customEmojiNames map[string]string
	userNames        map[string]string
	// markdown leaves no text where a data source mention of nothing the
	// writer may read was, as Notion's Markdown does; a block write's fails.
	markdown bool
	// held is whom the page written mentions already (notion_mention_held.go).
	held *notionHeldMentions
}

func newNotionMentionPreparation(
	app core.App, identity notionOAuthBearerIdentity, current pageAuthorization,
) *notionMentionPreparation {
	preparation := &notionMentionPreparation{
		app: app, identity: identity, current: current,
		pageTitles: map[string]string{}, databaseTitles: map[string]string{},
		customEmojiNames: map[string]string{}, userNames: map[string]string{},
	}
	if current.Page != nil {
		preparation.held = newNotionHeldMentions(app, current.Workspace.OwnerID, current.Page.Id)
	}
	return preparation
}

// notionNameMentions points here (notionSpansHere), checks and names the links and mentions of a database's name or
// description, a data source's or page's title a Public API connection writes, as a cell's are. Notion takes them all
// (seen 2026-11-10): a reachable page or database by its title, one it can't reach or that isn't there as an
// "Untitled" page mention by its ID. The name keeps a mention's target's title as a block's text does, which every
// reader reads as their own text reads it (page_name_mentions.go): the connection that can't reach it reads it
// "Untitled" (notionFormattedTitle), a person who can't open it in marks.
func notionNameMentions(
	app core.App, identity notionOAuthBearerIdentity, current pageAuthorization,
) func([]graphTextSpan) error {
	return notionSpansHere(app, current, newNotionMentionPreparation(app, identity, current).prepare)
}

func (preparation *notionMentionPreparation) prepare(spans []graphTextSpan) error {
	for spanIndex := range spans {
		span := &spans[spanIndex]
		if named, err := preparation.namedMention(span); err != nil {
			return err
		} else if !named {
			continue
		}
		switch span.Mention {
		case "":
			if span.PageID != "" || span.DatabaseID != "" || span.CustomEmojiID != "" || span.UserID != "" ||
				span.Date != "" || span.DateEnd != "" || span.DateTimeZone != "" {
				return invalidNotionRichTextMention()
			}
		case "page":
			if span.DatabaseID != "" || span.CustomEmojiID != "" ||
				span.UserID != "" || span.Date != "" || span.DateEnd != "" || span.DateTimeZone != "" {
				return invalidNotionRichTextMention()
			}
			// One written with a block's ID names the block's page at the block.
			block, anchored, err := anchorPageMention(preparation.app, preparation.current.Workspace.WorkspaceID, span)
			if err != nil {
				return err
			}
			if !anchored && notionUnresolvedPageMention(preparation.app, span.PageID) {
				span.Text = notionUnresolvedMentionTitle
				continue
			}
			if err := preparation.pageMention(span); err != nil {
				return err
			}
			if anchored {
				if err := preparation.blockMention(span, block); err != nil {
					return err
				}
			}
		case "database":
			if normalized, ok := normalizeNotionUUIDRequest(span.DatabaseID); !ok || normalized != span.DatabaseID ||
				span.PageID != "" || span.CustomEmojiID != "" || span.UserID != "" || span.Date != "" ||
				span.DateEnd != "" || span.DateTimeZone != "" {
				return invalidNotionRichTextMention()
			}
			if err := preparation.databaseMention(span); err != nil {
				return err
			}
		case "custom_emoji":
			if span.CustomEmojiID == "" {
				// A :name: of Markdown names one of the workspace's custom
				// emoji, or stays text.
				if err := resolveNotionMarkdownCustomEmoji(
					preparation.app, preparation.current.Workspace.WorkspaceID, span,
				); err != nil {
					return err
				}
				if span.Mention == "" {
					continue
				}
			}
			if span.CustomEmojiID == "" || span.PageID != "" || span.DatabaseID != "" ||
				span.UserID != "" || span.Date != "" || span.DateEnd != "" || span.DateTimeZone != "" {
				return invalidNotionRichTextMention()
			}
			publicID, err := notionCompatibleWorkspaceID(preparation.current.Workspace.WorkspaceID)
			if err != nil {
				return err
			}
			kept := notionMissingCustomEmojiKept(publicID, span.CustomEmojiID)
			emojiID := strings.ToLower(kept)
			name, found := preparation.customEmojiNames[emojiID]
			if !found {
				emoji, accessible, err := loadNotionCustomEmojiMention(
					preparation.app, preparation.identity.PrincipalID,
					preparation.current.Workspace.WorkspaceID, emojiID,
				)
				if err != nil {
					return err
				}
				if !accessible {
					// A custom emoji the workspace hasn't (or the writer
					// can't see) is kept by its ID as written, as Notion
					// keeps it in a title, a row's and a block's text alike
					// (seen 2026-11-10 and 2026-11-11;
					// notion_missing_custom_emoji.go).
					span.CustomEmojiID, span.Text = kept, notionMissingCustomEmojiText
					continue
				}
				name = strings.TrimSpace(emoji.Name)
				preparation.customEmojiNames[emojiID] = name
			}
			span.CustomEmojiID = emojiID
			if name == "" {
				return invalidNotionRichTextMention()
			}
			span.Text = ":" + name + ":"
		case "person":
			if err := preparation.personMention(span); err != nil {
				return err
			}
		case "date":
			if span.PageID != "" || span.DatabaseID != "" || span.CustomEmojiID != "" || span.UserID != "" {
				return invalidNotionRichTextMention()
			}
			// A date that isn't one is said as that, not as a mention out of
			// reach; Markdown keeps it as written, as Notion's does (seen
			// 2026-11-11, notionMarkdownDateSpan).
			if !preparation.markdown && !validNotionMentionDateRange(span.Date, span.DateEnd, span.DateTimeZone) {
				return graphInputError(errorCodeGraphInvalid, notionMentionDateFault(span.Date, span.DateEnd, span.DateTimeZone).Error())
			}
			span.Text = notionMarkdownDateText(span.Date, span.DateEnd)
		case notionMarkdownRefusedMention:
			return graphInputError(errorCodeGraphInvalid, span.Text)
		default:
			return invalidNotionRichTextMention()
		}
	}
	return nil
}

// pageMention names a page mention by its page, as Notion keeps each kind
// (seen 2026-11-09): one the writer may read by its title; one in the
// workspace the writer may not read linked to it all the same, with its
// title, which a reader who may not open it reads unnamed
// (mcp_reader_mentions.go), so whoever may open it reads it; one nothing
// here answers to by the ID written, "Untitled".
func (preparation *notionMentionPreparation) pageMention(span *graphTextSpan) error {
	written := span.PageID
	pageID, resolved := resolveNotionPageMentionID(preparation.app, span.PageID)
	if !resolved {
		if keepUnresolvedPageMention(span, written) {
			return nil
		}
		return invalidNotionRichTextMention()
	}
	span.PageID = pageID
	title, known := preparation.pageTitles[pageID]
	if !known {
		target, allowed, err := authorizeNotionPageMentionTarget(
			preparation.app, preparation.identity, preparation.current.Workspace.OwnerID,
			preparation.current.Workspace.WorkspaceID, pageID,
		)
		if err != nil {
			return err
		}
		title = target.Title
		if !allowed {
			now, workspaceID, there := pageMentionTargetNow(preparation.app, pageID)
			if !there || workspaceID != preparation.current.Workspace.WorkspaceID {
				if public, err := notionCompatiblePageID(pageID); err == nil && written != public &&
					!strings.HasPrefix(written, notionPageMentionRowPrefix) {
					written = public
				}
				if keepUnresolvedPageMention(span, written) {
					return nil
				}
				return invalidNotionRichTextMention()
			}
			title = now
		}
		// A page with no title is named as Notion's API names one, whatever
		// its writer's language (seen 2026-11-14: "New page").
		if title == "" {
			title = notionUntitledPageMentionTitle
		}
		preparation.pageTitles[pageID] = title
	}
	span.Text = title
	return nil
}

// blockMention names a mention of a block by the block's words as the
// writer reads them, "Untitled" for none or for a block on a page the writer
// may not read (page_mention_block_anchor.go).
func (preparation *notionMentionPreparation) blockMention(span *graphTextSpan, block storedNotionBlock) error {
	ownerID, workspaceID := preparation.current.Workspace.OwnerID, preparation.current.Workspace.WorkspaceID
	_, allowed, err := authorizeNotionPageMentionTarget(preparation.app, preparation.identity, ownerID, workspaceID, block.PageID)
	if err != nil {
		return err
	}
	span.Text = ""
	if allowed {
		span.Text = notionBlockMentionWords(preparation.app, preparation.identity, ownerID, workspaceID, block)
	}
	if span.Text == "" {
		span.Text = notionUnresolvedMentionTitle
	}
	return nil
}

// databaseMention names a database mention by its database as pageMention names a page's: one the writer may not
// read is linked with its name; one nothing in the workspace answers to is an "Untitled" page mention by the ID
// written, as Notion's API reads either back; one by a data source's address, as an MCP value carries one, the source's.
func (preparation *notionMentionPreparation) databaseMention(span *graphTextSpan) error {
	written := span.DatabaseID
	containerID, found, err := preparation.databaseContainerID(span.DatabaseID)
	if err != nil {
		return err
	}
	if !found {
		if keepUnresolvedPageMention(span, written) {
			return nil
		}
		return invalidNotionRichTextMention()
	}
	if named, err := dataSourceAddressMention(preparation.app, span, containerID); named || err != nil {
		return err
	}
	span.DatabaseID = containerID
	title, known := preparation.databaseTitles[containerID]
	if !known {
		var allowed bool
		if title, allowed, err = notionDatabaseMentionTitle(
			preparation.app, preparation.identity, preparation.current, containerID,
		); err != nil {
			return err
		}
		if !allowed {
			if title, _, err = databaseContainerTitle(preparation.app, containerID); err != nil {
				return err
			}
			if strings.TrimSpace(title) == "" {
				title = notionUnresolvedMentionTitle
			}
		}
		preparation.databaseTitles[containerID] = title
	}
	if strings.TrimSpace(title) == "" {
		title = notionUntitledDatabaseMentionTitle
	}
	span.Text = title
	return nil
}

// notionUntitledDatabaseMentionTitle is what a mention of a database with no
// name reads, as Notion's API gives it (seen 2026-11-10).
const notionUntitledDatabaseMentionTitle = "New database"

// databasePageMention turns a mention of a page that is a database, as
// Notion's Markdown writes a database mention, into a mention of the database.
func (preparation *notionMentionPreparation) databasePageMention(span *graphTextSpan) error {
	if span.Mention != "page" {
		return nil
	}
	publicID := strings.TrimPrefix(span.PageID, notionPageMentionRowPrefix)
	if _, resolved := resolveNotionPageMentionID(preparation.app, span.PageID); resolved {
		return nil
	}
	containerID, found, err := preparation.databaseContainerID(publicID)
	if err != nil || !found {
		return err
	}
	span.Mention, span.PageID, span.DatabaseID = "database", "", containerID
	return nil
}

// pageDatabaseMention turns page Markdown's mention of a database by an ID
// no database has — a page's address of type="database", or in a
// <mention-database> — into a mention of the page the ID names, or of
// nothing, as Notion's Markdown reads an address by its target (seen
// 2026-11-11: a page's mention, or an "Untitled" one).
func (preparation *notionMentionPreparation) pageDatabaseMention(span *graphTextSpan) error {
	if _, named := notionMarkdownDataSource(*span); !preparation.markdown || span.Mention != "database" || named {
		return nil
	}
	if _, found, err := preparation.databaseContainerID(span.DatabaseID); err != nil || found {
		return err
	}
	request := notionMentionRequest{Type: "page"}
	request.Page = &struct {
		ID string `json:"id"`
	}{ID: span.DatabaseID}
	converted, err := notionMentionRequestGraphSpan(request)
	if err != nil {
		return nil
	}
	span.Mention, span.PageID, span.DatabaseID = "page", converted.PageID, ""
	return nil
}

// databaseContainerID finds the workspace's database a database ID names:
// its Public API ID, or the ID of the database itself.
func (preparation *notionMentionPreparation) databaseContainerID(databaseID string) (string, bool, error) {
	identity, err := resolveNotionObjectIdentity(preparation.app, notionObjectIdentityDatabase, databaseID)
	if err == nil {
		if identity.Workspace != preparation.current.Workspace.WorkspaceID {
			return "", false, nil
		}
		return identity.InternalID, true, nil
	}
	if !errors.Is(err, errInvalidNotionObjectIdentity) {
		return "", false, err
	}
	// By its own ID, one in the trash too (notionMentionedDatabase).
	database, found, err := loadNotionMentionedDatabase(preparation.app, databaseID)
	if err != nil {
		return "", false, err
	}
	return databaseID, found && database.WorkspaceID == preparation.current.Workspace.WorkspaceID, nil
}

func loadNotionMentionUser(
	app core.App,
	workspaceID string,
	userID string,
) (storedNotionDirectoryUser, bool, error) {
	rows := []storedNotionDirectoryUser{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]], [[user]].[[name]],
			[[user]].[[profile_avatar]] AS [[avatar]], [[user]].[[email]],
			[[user]].[[verified]] AS [[email_verified]]
		FROM {{users}} [[user]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
		WHERE [[user]].[[id]] = {:userId} AND ` + notionPersonInWorkspaceSQL + `
		LIMIT 2
	`).Bind(map[string]any{"workspaceId": workspaceID, "userId": userID}).All(&rows)
	if err != nil || len(rows) != 1 {
		return storedNotionDirectoryUser{}, false, err
	}
	return rows[0], true, nil
}

// notionPersonMentionWords is what a mention of a person here says: "@"
// and their name, as Notion's plain_text says it, or their public ID when
// they have none. A block's, a property's and a comment's text keep these
// words, which a notice's line, search and an export read.
func notionPersonMentionWords(name, userID string) string {
	if name = strings.TrimSpace(name); name == "" {
		name, _ = notionCompatibleUserID(userID)
	}
	return "@" + strings.TrimPrefix(name, "@")
}

func invalidNotionRichTextMention() error {
	return graphInputError(errorCodeGraphReferenceInvalid, "The rich_text mention is not accessible.")
}
