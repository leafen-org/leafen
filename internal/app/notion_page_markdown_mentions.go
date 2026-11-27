package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's Markdown names what its mentions point to as its reader does, and
// the mentions of Markdown written to a page are resolved as its writer
// names them and checked as the writer's block writes are.

// mentions names what a page's mentions point to as the reader does; a
// failure to read that is kept in failed.
func (reader notionMarkdownReader) mentions(app core.App, workspaceID string, failed *error) notionMarkdownMentions {
	readable := func(pageID string) bool {
		if reader.Readable == nil {
			return true
		}
		ok, err := reader.Readable(app, pageID)
		if err != nil && *failed == nil {
			*failed = err
		}
		return ok
	}
	people, targets := map[string]string{}, readerMentionTargets(app, failed)
	mentions := notionMarkdownMentions{
		Origin: reader.Origin, APIForm: reader.APIForm, Version2: reader.Version2, Discussions: reader.Discussions,
		PageMissing: func(pageID string) bool { return targets.missing(app, pageID) },
		PageTitle:   func(pageID string) string { return reader.mentionPageTitle(app, workspaceID, targets, pageID) },
		DatabaseTitle: func(databaseID string) string {
			title, _, err := databaseContainerTitle(app, databaseID)
			if err != nil && *failed == nil {
				*failed = err
			}
			return title
		},
		SyncedPage: func(blockID string) string {
			pageID := ""
			if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": blockID}).Row(&pageID); err != nil || pageID == "" || !readable(pageID) {
				return ""
			}
			if reader.InternalIDs {
				return pageID
			}
			if publicID, err := notionPagePublicID(app, pageID); err == nil {
				return publicID
			}
			return ""
		},
		SyncedBlocks: func(blockID string) ([]workspaceExportMarkdownBlock, bool) {
			return notionMarkdownSyncedBlocks(app, blockID, readable)
		},
		SyncedPageBody: func(pageID string) notionMarkdownSyncedPage {
			return notionMarkdownSyncedPageBody(app, pageID, readable, reader.InternalIDs)
		},
		UserName: func(userID string) string {
			if name, known := people[userID]; known {
				return name
			}
			names, err := personMentionNames(app, workspaceID, []string{userID})
			if err != nil && *failed == nil {
				*failed = err
			}
			people[userID] = names[userID]
			return people[userID]
		},
		TextLink: reader.textLink(app, workspaceID, readable),
		Page: func(pageID string) (string, bool) {
			// An ID no page answers to reads by that ID, undashed, by the MCP too (seen 2026-11-11).
			if targets.missing(app, pageID) {
				return notionMentionURL(reader.Origin, pageID), true
			}
			if pageID == "" || !readable(pageID) {
				return "", false
			}
			if reader.InternalIDs {
				return notionMentionURL(reader.Origin, pageID), true
			}
			publicID, err := notionPagePublicID(app, pageID)
			if err != nil {
				return "", false
			}
			return notionMentionURL(reader.Origin, publicID), true
		},
		// One the reader can't open reads by its ID, as Notion's API gives it.
		Unnamed: func(mention, targetID string) (string, bool) {
			if targetID == "" {
				return "", false
			}
			if reader.InternalIDs {
				return notionMentionURL(reader.Origin, targetID), true
			}
			publicID, err := notionPagePublicID(app, targetID)
			if mention == "database" {
				publicID, err = notionDatabaseMentionPublicID(app, workspaceID, targetID)
			}
			return notionMentionURL(reader.Origin, publicID), err == nil
		},
		// One in the trash reads as any (authorizeNotionDatabaseMentionTarget).
		Database: func(databaseID string) (string, bool) {
			database, found, err := loadNotionMentionedDatabase(app, databaseID)
			if err != nil {
				if *failed == nil {
					*failed = err
				}
				return "", false
			}
			if !found || !readable(database.PageID) {
				return "", false
			}
			// By Leafen's IDs at its one address (databaseAppAddress).
			if reader.InternalIDs {
				return databaseAppAddress(reader.Origin, databaseID), true
			}
			publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, workspaceID, databaseID)
			if err != nil {
				if *failed == nil {
					*failed = err
				}
				return "", false
			}
			return notionMentionURL(reader.Origin, publicID), true
		},
		CustomEmoji: func(emojiID string) (string, bool) {
			names := []string{}
			if err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_custom_emojis}}
				WHERE [[workspace_id]]={:workspaceId} AND [[id]]={:id} AND [[workspace_import_job_id]]='' LIMIT 1`).
				Bind(dbx.Params{"workspaceId": workspaceID, "id": emojiID}).Column(&names); err != nil {
				if *failed == nil {
					*failed = err
				}
				return "", false
			}
			if len(names) == 0 {
				return "", false
			}
			return names[0], true
		},
		// One the workspace hasn't reads by its workspace's public ID and its own, as Notion's (notion_missing_custom_emoji.go).
		MissingCustomEmoji: func(emojiID string) string {
			publicID, err := notionCompatibleWorkspaceID(workspaceID)
			if err != nil {
				return ""
			}
			return notionMissingCustomEmojiAddress(publicID, emojiID)
		},
		Person:      reader.person(app, workspaceID, failed),
		FileAddress: reader.fileAddresses(app, workspaceID),
		DataSource:  func(id string) string { return notionMarkdownDataSourceID(app, id) },
		Targets:     targets,
	}
	mentions.Block = reader.blockMention(app, workspaceID, mentions, readable)
	return mentions
}

// notionMarkdownPublicMentions turns the mentions of Markdown a Public API
// connection writes, which name their targets by the connection's IDs — the
// addresses of this server's pages standing bare in its text too
// (notionMarkdownSitePages) — into the page's own; the mention preparation
// then checks them.
func notionMarkdownPublicMentions(updates []graphTextUpdate) error {
	for updateIndex := range updates {
		for spanIndex := range updates[updateIndex].Spans {
			span := &updates[updateIndex].Spans[spanIndex]
			request := notionMentionRequest{}
			written := ""
			var err error
			switch span.Mention {
			case "page":
				err = json.Unmarshal([]byte(`{"type":"page","page":{"id":`+strconv.Quote(span.PageID)+`}}`), &request)
			case "person":
				// Its ID in either case names the person.
				id := strings.TrimSpace(span.UserID)
				if normalized, isUUID := normalizeNotionUUIDRequest(id); isUUID {
					id = normalized
				}
				written = span.UserID
				err = json.Unmarshal([]byte(`{"type":"user","user":{"id":`+strconv.Quote(id)+`}}`), &request)
			case "database":
				if _, named := notionMarkdownAddressed(*span); named {
					// The page's mention preparation finds the source's database.
					continue
				}
				err = json.Unmarshal([]byte(`{"type":"database","database":{"id":`+strconv.Quote(span.DatabaseID)+`}}`), &request)
			default:
				continue
			}
			if err != nil {
				return invalidNotionRichTextMention()
			}
			converted, err := notionMentionRequestGraphSpan(request)
			// A person no one here is — a connection's bot too — stays
			// mentioned by the ID as written, as Notion's Markdown keeps one
			// (notion_markdown_person_mentions.go).
			if refusal := (&notionMentionRefusal{}); span.Mention == "person" && errors.As(err, &refusal) && refusal.missingUser != "" {
				span.UserID, _ = notionKeptUUID(written)
				continue
			}
			if err != nil {
				return invalidNotionRichTextMention()
			}
			span.PageID, span.UserID, span.DatabaseID = converted.PageID, converted.UserID, converted.DatabaseID
			if normalized, _ := normalizeNotionUUIDRequest(written); span.Mention == "person" && converted.UserID == normalized {
				span.UserID, _ = notionKeptUUID(written)
			}
		}
	}
	return nil
}

// notionMarkdownPublicMentionInput prepares the mentions of Markdown a
// Public API connection writes to a page, as its block writes do.
func notionMarkdownPublicMentionInput(
	identity notionOAuthBearerIdentity,
) func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error) {
	return func(app core.App, current pageAuthorization, _ *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
		if err := notionMarkdownPublicMentions(input.TextUpdates); err != nil {
			return graphMutationInput{}, err
		}
		// A caption's and a table cell's mentions are its block's, which
		// prepare after.
		captions, put := notionMarkdownSpanFields(input.Operations)
		if err := notionMarkdownPublicMentions(captions); err != nil {
			return graphMutationInput{}, err
		}
		if err := put(); err != nil {
			return graphMutationInput{}, err
		}
		if err := resolveNotionMarkdownCalloutIcons(app, current.Workspace.WorkspaceID, input.Operations); err != nil {
			return graphMutationInput{}, err
		}
		return prepareNotionMentionsInput(app, identity, current, input, true)
	}
}

// principalMarkdownMentionInput checks the page mentions of Markdown someone
// writes through the MCP tools — in its text, captions and table cells: each
// names a page of the workspace, and shows its title; a database mention
// names one of its databases by its name (principalDatabaseMention); a data
// source mention is the source's (principalDataSourceMention); a person mention names its person
// by their Leafen ID and reads "@" and their name, as a person's typed one
// does, so what is told of it — a notice, its mail, a bot's word — names
// them (resolveMarkdownPersonMentions). Its :name: become the workspace's
// custom emoji, and a piece its reading refuses refuses the write
// (notionMarkdownWrittenMentions).
func principalMarkdownMentionInput(
	principalID string,
) func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error) {
	return narrowedMarkdownMentionInput(principalID, nil)
}

// narrowedMarkdownMentionInput is principalMarkdownMentionInput for a writer
// narrowed to some of the pages its person reads (an agent's token narrowed
// to chosen pages): reaches keeps the pages and databases it may name, and
// one outside them is kept as one the writer can't read is.
func narrowedMarkdownMentionInput(
	principalID string, reaches func(core.App, pageAuthorization) bool,
) func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error) {
	return func(app core.App, current pageAuthorization, _ *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
		allows := func(target pageAuthorization) bool { return reaches == nil || reaches(app, target) }
		// A caption's and a table cell's mentions are checked as its block's
		// text is.
		captions, put := notionMarkdownSpanFields(input.Operations)
		updates := slices.Concat(input.TextUpdates, captions)
		if err := notionMarkdownWrittenMentions(app, current, updates, input.Operations); err != nil {
			return graphMutationInput{}, err
		}
		for updateIndex := range updates {
			for spanIndex := range updates[updateIndex].Spans {
				span := &updates[updateIndex].Spans[spanIndex]
				if _, named := notionMarkdownAddressed(*span); named {
					if err := principalDataSourceMention(app, principalID, current, span, allows); err != nil {
						return graphMutationInput{}, err
					}
					continue
				}
				if err := principalDatabaseMention(app, principalID, current, span, allows); err != nil {
					return graphMutationInput{}, err
				}
				if span.Mention != "page" {
					continue
				}
				// One written by the ID the public API names its page by
				// (/p/<UUID>), which the app opens too (mcpInternalPageID),
				// names that page.
				if own, err := resolvePageOwnPublicID(notionLinkedID(span.PageID)); err == nil && own != span.PageID {
					if _, _, there := pageMentionTargetNow(app, own); there {
						span.PageID = own
					}
				}
				// One written with a block's ID names the block's page at the
				// block, by the block's words (page_mention_block_anchor.go).
				block, anchored, err := anchorPageMention(app, current.Workspace.WorkspaceID, span)
				if err != nil {
					return graphMutationInput{}, err
				}
				target, allowed, err := authorizePageMentionTarget(
					app, principalID, current.Workspace.OwnerID, current.Workspace.WorkspaceID, span.PageID, allows,
				)
				if err != nil {
					return graphMutationInput{}, err
				}
				if anchored {
					span.Text = notionUnresolvedMentionTitle
					if allowed {
						nameAnchoredPageMention(app, principalID, current.Workspace.WorkspaceID, span, block)
					}
					continue
				}
				if !allowed {
					// A page of the workspace the writer can't reach stays
					// linked to it with its title, which only a reader who may
					// open it reads, as the Public API's writes keep one
					// (notionMentionPreparation.pageMention); one that isn't
					// there stays a mention of it named "Untitled", as
					// Notion's MCP keeps one (seen 2026-11-11, worker w297).
					now, workspaceID, there := pageMentionTargetNow(app, span.PageID)
					if !there || workspaceID != current.Workspace.WorkspaceID {
						if !keepUnresolvedPageMention(span, span.PageID) {
							span.Mention, span.DatabaseID, span.Text = "page", "", notionUnresolvedMentionTitle
						}
						continue
					}
					target.Title = now
				}
				// A page with no title is named as Notion's API names one
				// ("New page"), whatever its writer's language.
				if span.Text = target.Title; span.Text == "" {
					span.Text = notionUntitledPageMentionTitle
				}
			}
		}
		return input, put()
	}
}

// notionMarkdownSpanFields are the captions and table cells holding mentions
// of the blocks a Markdown change writes, as text updates their mentions are
// checked by, as a block's text is; put writes them back to their blocks.
func notionMarkdownSpanFields(operations []graphOperation) ([]graphTextUpdate, func() error) {
	mentioned := func(spans []graphTextSpan) bool {
		return slices.ContainsFunc(spans, func(span graphTextSpan) bool { return span.Mention != "" })
	}
	updates := []graphTextUpdate{}
	// Each update's block, and its cell: none for a caption.
	blocks, cells := []*graphBlock{}, []int{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil {
			continue
		}
		if operation.Block.Type == graphBlockTypeTableRow {
			row, err := parseGraphTableRowBlockContent(operation.Block.Content)
			if err != nil {
				continue
			}
			for index, cell := range row.Cells {
				if mentioned(cell) {
					updates = append(updates, graphTextUpdate{BlockID: operation.Block.ID, Spans: cell})
					blocks, cells = append(blocks, operation.Block), append(cells, index)
				}
			}
			continue
		}
		content := struct {
			Caption []graphTextSpan `json:"caption"`
		}{}
		if json.Unmarshal(operation.Block.Content, &content) != nil || !mentioned(content.Caption) {
			continue
		}
		updates = append(updates, graphTextUpdate{BlockID: operation.Block.ID, Spans: content.Caption})
		blocks, cells = append(blocks, operation.Block), append(cells, -1)
	}
	return updates, func() error {
		for index, block := range blocks {
			var err error
			if cells[index] >= 0 {
				row, err := parseGraphTableRowBlockContent(block.Content)
				if err != nil {
					return err
				}
				row.Cells[cells[index]] = updates[index].Spans
				if block.Content, err = json.Marshal(row); err != nil {
					return err
				}
				continue
			}
			fields := map[string]json.RawMessage{}
			if err := json.Unmarshal(block.Content, &fields); err != nil {
				return err
			}
			if fields["caption"], err = json.Marshal(updates[index].Spans); err != nil {
				return err
			}
			if block.Content, err = json.Marshal(fields); err != nil {
				return err
			}
		}
		return nil
	}
}

// resolveNotionMarkdownCustomEmoji turns a :name: of Markdown written to a
// page into a mention of the workspace's custom emoji with that name; a name
// no custom emoji has stays text.
func resolveNotionMarkdownCustomEmoji(app core.App, workspaceID string, span *graphTextSpan) error {
	emojis := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}{}
	if _, key, err := normalizeCustomEmojiName(strings.TrimSuffix(strings.TrimPrefix(span.Text, ":"), ":")); err == nil {
		if err := app.DB().NewQuery(`SELECT [[id]], [[name]] FROM {{leafen_custom_emojis}}
			WHERE [[workspace_id]]={:workspaceId} AND [[name_key]]={:key} AND [[workspace_import_job_id]]='' LIMIT 1`).
			Bind(dbx.Params{"workspaceId": workspaceID, "key": key}).All(&emojis); err != nil {
			return err
		}
	}
	if len(emojis) == 0 {
		span.Mention = ""
		return nil
	}
	span.CustomEmojiID, span.Text = emojis[0].ID, ":"+emojis[0].Name+":"
	return nil
}

// resolveNotionMarkdownCustomEmojis resolves the :name: of each text update.
func resolveNotionMarkdownCustomEmojis(app core.App, workspaceID string, updates []graphTextUpdate) error {
	for updateIndex := range updates {
		for spanIndex := range updates[updateIndex].Spans {
			span := &updates[updateIndex].Spans[spanIndex]
			if span.Mention != "custom_emoji" || span.CustomEmojiID != "" {
				continue
			}
			if err := resolveNotionMarkdownCustomEmoji(app, workspaceID, span); err != nil {
				return err
			}
		}
	}
	return nil
}

// notionMarkdownSyncedBlocks are the blocks a synced block holds, on a page the reader may open, with the text they
// stand with: in the tree's order (each block's path of places from the synced block down), as many as the
// Markdown's records take, and whether it holds more, which the Markdown says as truncated.
//
// The walk steps from each block it holds to that block's children by the
// page's order index (owner, page, parent), in time with the blocks it
// reaches: without the owner, SQLite read every block of the original's page
// for each block, and 20,100 blocks took minutes while every write waited
// (2026-11-08). It takes the blocks in the tree's order and stops one past the records the Markdown takes,
// so a larger original isn't walked to its end.
func notionMarkdownSyncedBlocks(app core.App, blockID string, readable func(string) bool) ([]workspaceExportMarkdownBlock, bool) {
	pageID, owner := "", ""
	if err := app.DB().NewQuery(`SELECT [[page_id]], [[owner]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": blockID}).Row(&pageID, &owner); err != nil || pageID == "" || !readable(pageID) {
		return nil, false
	}
	rows := []storedStructuralBlock{}
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[held]]([[id]], [[path]]) AS (
			SELECT [[id]], [[position]] || char(1) || [[id]] FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[parent_id]] = {:id}
			UNION
			SELECT [[block]].[[id]], [[held]].[[path]] || char(2) || [[block]].[[position]] || char(1) || [[block]].[[id]]
			FROM [[held]] CROSS JOIN {{leafen_blocks}} AS [[block]]
				ON [[block]].[[owner]] = {:owner} AND [[block]].[[page_id]] = {:pageId} AND [[block]].[[parent_id]] = [[held]].[[id]]
			WHERE instr([[held]].[[path]] || char(2), char(1) || [[block]].[[id]] || char(2)) = 0
			ORDER BY 2
			LIMIT {:limit}
		)
		SELECT [[block]].[[id]], [[block]].[[parent_id]], [[block]].[[position]], [[block]].[[type]], [[block]].[[content]]
		FROM [[held]] CROSS JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]] = [[held]].[[id]]
		ORDER BY [[held]].[[path]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "id": blockID, "limit": workspaceExportNotionMarkdownRecordLimit + 1,
	}).All(&rows); err != nil {
		return nil, false
	}
	more := len(rows) > workspaceExportNotionMarkdownRecordLimit
	if more {
		rows = rows[:workspaceExportNotionMarkdownRecordLimit]
	}
	document, found, err := loadYgoDoc(app, pageID)
	if err != nil {
		return nil, false
	}
	blocks := make([]workspaceExportMarkdownBlock, 0, len(rows))
	for _, row := range rows {
		block := workspaceExportMarkdownBlock{
			ID: row.ID, ParentID: row.ParentID, Position: row.Position, Type: row.Type, Content: map[string]any{},
		}
		_ = json.Unmarshal([]byte(row.Content), &block.Content)
		if found {
			if spans, text := rowPageBlockTextAsItStands(document, row.ID, false); text != "" {
				block.Content["text"], block.Spans = text, spans
			}
		}
		blocks = append(blocks, block)
	}
	return blocks, more
}
