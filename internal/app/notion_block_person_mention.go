package app

import "strings"

// personMention checks and names a person mention a write makes: a person
// of the workspace by "@" and their name; a bot of the workspace's
// connections, or the writing token's own, by its ID and its name; one the
// page holds already as it is held (notion_mention_held.go); anyone else as
// Notion keeps or refuses them.
func (preparation *notionMentionPreparation) personMention(span *graphTextSpan) error {
	if span.UserID == "" || span.PageID != "" || span.DatabaseID != "" || span.CustomEmojiID != "" ||
		span.Date != "" || span.DateEnd != "" || span.DateTimeZone != "" {
		return invalidNotionRichTextMention()
	}
	name, found := preparation.userNames[span.UserID]
	if !found {
		user, accessible, err := loadNotionMentionUser(
			preparation.app, preparation.current.Workspace.WorkspaceID, span.UserID,
		)
		if err != nil {
			return err
		}
		if !accessible {
			// A bot of the workspace's connections, or the writing
			// token's own, is kept by its ID and named as it is, as
			// in Notion (seen 2026-11-10); page Markdown keeps one as
			// a mention of no one, as Notion's Markdown does (seen
			// 2026-11-11: "@Anonymous").
			if bot, found := notionMentionBotFor(preparation.app, preparation.identity, preparation.current.Workspace.WorkspaceID, span.UserID); found && !preparation.markdown {
				preparation.userNames[span.UserID] = bot.Name
				span.UserID, span.Text = bot.ID, "@"+bot.Name
				return nil
			}
			// One the page holds already stays as it is held, whoever
			// writes it back and however.
			if userID, words, held, err := preparation.held.mention(span.UserID); err != nil || held {
				if held {
					span.UserID, span.Text = userID, words
				}
				return err
			}
			publicID, err := notionCompatibleUserID(span.UserID)
			if notionBotUserIDShape(span.UserID) {
				publicID, err = span.UserID, nil
			}
			// Markdown keeps a mention of someone who isn't here by
			// the ID written, as Notion's does (seen 2026-11-11);
			// one who has left, by their public ID, as one no person
			// here has (notionUnknownPersonMention), whom the editor
			// draws so.
			if _, isUUID := normalizeNotionUUIDRequest(span.UserID); preparation.markdown && (err == nil || isUUID) {
				if isUUID {
					preparation.userNames[span.UserID] = strings.TrimPrefix(notionUnknownPersonMentionText, "@")
				} else {
					span.UserID = publicID
				}
				span.Text = notionUnknownPersonMentionText
				return nil
			}
			if err != nil {
				return invalidNotionRichTextMention()
			}
			return &notionMentionRefusal{missingUser: publicID}
		}
		name = strings.TrimSpace(user.Name)
		preparation.userNames[span.UserID] = name
	}
	span.Text = notionPersonMentionWords(name, span.UserID)
	return nil
}
