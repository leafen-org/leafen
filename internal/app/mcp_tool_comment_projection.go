package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A page's discussions read as Notion's MCP writes them (seen 2026-11-11 and
// 11-22): open ones before resolved ones, and of each those with the most
// comments first, between as many the page's own first, then those on its
// blocks in the order the page shows the blocks, each block's in the order
// they were started. A
// discussion names where it is (context: page, block, or inline with the
// words it was left on as text-context), and each comment its address —
// the page opened at its discussion — its author as user://ID (with their
// email in get-comments, where the reader's directory gives it), its time,
// whether the reader wrote it (get-comments), and its text as inline "v2"
// page Markdown.

// notionMCPDiscussions are the discussions get-comments lists for its
// input: the open ones, or all with include_resolved, or the one it names;
// those of them window writes (mcp_tool_comments_window.go).
func notionMCPDiscussions(
	app core.App,
	authority mcpAuthority,
	target mcpCommentTargetAuthorization,
	records []commentRecord,
	input notionMCPGetCommentsInput,
	window *mcpDiscussionWindow,
) ([]notionMCPDiscussion, error) {
	wantedRoot := ""
	if strings.TrimSpace(input.DiscussionID) != "" {
		root, found, err := resolveMCPDiscussionRoot(app, authority, target.PageID, input.DiscussionID, records)
		if err != nil {
			return nil, err
		}
		if !found {
			return []notionMCPDiscussion{}, nil
		}
		wantedRoot = root.ID
	}
	discussions, _, err := mcpPageDiscussions(app, authority, target, records, func(root commentRecord) bool {
		if wantedRoot != "" {
			return root.ID == wantedRoot
		}
		return input.IncludeResolved || !root.Resolved
	}, true, wantedRoot != "", window)
	return discussions, err
}

// mcpPageDiscussions are a page's discussions whose first comment wanted
// keeps, in the order above, as their reader reads them: get-comments
// (listed) or fetch (newMCPCommentReader); one asked for by its ID, or those
// a list names (mcp_discussion_places.go), and where they stand. Of them,
// those window writes are read out (all for none).
func mcpPageDiscussions(
	app core.App,
	authority mcpAuthority,
	target mcpCommentTargetAuthorization,
	records []commentRecord,
	wanted func(commentRecord) bool, listed, asked bool,
	window *mcpDiscussionWindow,
) ([]notionMCPDiscussion, mcpDiscussionPlaces, error) {
	// Each comment's files, for its counts, and its reactions, as the app shows them.
	if err := attachMCPCommentParts(app, records); err != nil {
		return nil, mcpDiscussionPlaces{}, failedBecause(errMCPToolFailed, err)
	}
	replies := map[string][]commentRecord{}
	roots := make([]commentRecord, 0, len(records))
	for _, record := range records {
		switch {
		case record.ParentID != "":
			replies[record.ParentID] = append(replies[record.ParentID], record)
		case wanted(record):
			roots = append(roots, record)
		}
	}
	earlier := func(left, right commentRecord) bool {
		if left.CreatedAt != right.CreatedAt {
			return left.CreatedAt < right.CreatedAt
		}
		return left.ID < right.ID
	}
	places := mcpPlaceDiscussions(app, target.Authorization.Target.ID, roots)
	roots = places.kept(roots, asked)
	sort.SliceStable(roots, func(left, right int) bool {
		if roots[left].Resolved != roots[right].Resolved {
			return !roots[left].Resolved
		}
		if many, more := len(replies[roots[left].ID]), len(replies[roots[right].ID]); many != more {
			return many > more
		}
		if places.first(roots[left]) != places.first(roots[right]) {
			return places.first(roots[left]) < places.first(roots[right])
		}
		return earlier(roots[left], roots[right])
	})
	reader := newMCPCommentReader(app, authority, target.Authorization.Page.Workspace.WorkspaceID, listed)
	start, err := window.start(roots)
	if err != nil {
		return nil, mcpDiscussionPlaces{}, err
	}
	result := make([]notionMCPDiscussion, 0, len(roots)-start)
	for _, root := range roots[start:] {
		thread := append([]commentRecord{root}, replies[root.ID]...)
		sort.SliceStable(thread[1:], func(left, right int) bool { return earlier(thread[1+left], thread[1+right]) })
		discussion := notionMCPDiscussion{
			DiscussionID: mcpDiscussionID(app, root.WorkspaceID, target.PageID, places.named(root, asked), root.ID), RootID: root.ID,
			BlockID: root.BlockID, Resolved: root.Resolved, Context: "page",
		}
		if body := (suggestionThreadBody{}); json.Unmarshal(root.Body, &body) == nil && body.Suggestion.Kind != "" && body.Suggestion.State == "" {
			discussion.Kind = "suggested_edit"
		}
		if root.BlockID != "" {
			discussion.Context = "block"
			anchor := commentQuoteAnchor{}
			if json.Unmarshal(root.Body, &anchor) == nil && strings.TrimSpace(anchor.Quote) != "" {
				// One on the block itself keeps its context with the block's words (onWords).
				discussion.TextContext = anchor.Quote
				if anchor.onWords() {
					discussion.Context = places.context(root, asked)
				}
				// One on a table's cell words is the row's (comment_table_cell_anchors.go).
				if _, cell, _ := commentQuoteFieldPlace(anchor.QuoteField); cell > 0 && discussion.Context != "" {
					discussion.Context = "block"
				}
			}
		}
		address := mcpPageURL(authority, target.PageID) + "#comment-" + url.PathEscape(root.ID)
		for _, record := range thread {
			comment, err := reader.comment(record)
			if err != nil {
				return nil, mcpDiscussionPlaces{}, failedBecause(errMCPToolFailed, err)
			}
			comment.URL = address
			discussion.Comments = append(discussion.Comments, comment)
		}
		if takes, err := window.takes(discussion, len(result)); err != nil || !takes {
			return result, places, err
		}
		result = append(result, discussion)
	}
	return result, places, nil
}

func resolveMCPDiscussionRoot(
	app core.App, authority mcpAuthority, pageID, raw string,
	records []commentRecord,
) (commentRecord, bool, error) {
	wantedPage, wantedBlock, wantedID, err := parseMCPDiscussionID(app, authority, raw)
	if err != nil || wantedPage != "" && wantedPage != pageID {
		return commentRecord{}, false, mcpInvalid("discussion_id isn't a discussion on this page: notion-get-comments lists them")
	}
	root, found, err := mcpCommentThreadRoot(wantedID, records)
	if err != nil || !found {
		return commentRecord{}, false, err
	}
	// One whose words' marks stand in another block now is found by that block's address too.
	if wantedPage != "" && root.BlockID != wantedBlock && !mcpPlaceDiscussions(app, pageID, []commentRecord{root}).marks(root.ID, wantedBlock) {
		return commentRecord{}, false, mcpInvalid("discussion_id isn't a discussion on that block: notion-get-comments lists them")
	}
	return root, true, nil
}

// mcpCommentThreadRoot is the first comment of the discussion a comment is
// in, found among its page's comments; false for one not there.
func mcpCommentThreadRoot(id string, records []commentRecord) (commentRecord, bool, error) {
	byID := make(map[string]commentRecord, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	root, found := byID[id]
	if !found {
		return commentRecord{}, false, nil
	}
	seen := map[string]struct{}{}
	for root.ParentID != "" {
		if _, duplicate := seen[root.ID]; duplicate {
			return commentRecord{}, false, errMCPToolFailed
		}
		seen[root.ID] = struct{}{}
		parent, ok := byID[root.ParentID]
		if !ok {
			return commentRecord{}, false, errMCPToolFailed
		}
		root = parent
	}
	return root, true, nil
}

// mcpCommentReader reads comments for an agent: its mentions as the
// connection's Markdown names them (a page or database closed to it
// unnamed), each author once.
type mcpCommentReader struct {
	app         core.App
	authority   mcpAuthority
	workspaceID string
	listed      bool
	mentions    notionMarkdownMentions
	authors     map[string]mcpCommentAuthor
}

type mcpCommentAuthor struct {
	id, email string
}

// comment is a comment as its reader reads it, without its address.
func (reader *mcpCommentReader) comment(record commentRecord) (notionMCPComment, error) {
	var body struct {
		Rich []mcpCommentSpan `json:"rich"`
	}
	if json.Unmarshal(record.Body, &body) != nil || body.Rich == nil {
		return notionMCPComment{}, errCommentFailed
	}
	author, known := reader.authors[record.AuthorID]
	if !known {
		id, err := notionCompatibleUserID(record.AuthorID)
		if err != nil {
			return notionMCPComment{}, err
		}
		author.id = id
		user, found, err := loadNotionUserDirectoryUser(reader.app, reader.authority.PrincipalID, reader.workspaceID, record.AuthorID)
		if err != nil {
			return notionMCPComment{}, err
		}
		if found {
			author.email = user.Email
		}
		reader.authors[record.AuthorID] = author
	}
	comment := notionMCPComment{
		ID: mcpCommentID(reader.app, record.WorkspaceID, record.ID), CreatedTime: personClockMillisOf(record.CreatedAt, reader.authority.clock()),
		UserURL: "user://" + author.id, Email: author.email,
		ByYou: record.AuthorID == reader.authority.PrincipalID,
		Body:  mcpCommentMarkdown(reader.app, body.Rich, reader.mentions, reader.listed),
	}
	comment.Reactions, comment.YourReactions = mcpCommentReactions(record.Reactions, reader.authority.PrincipalID)
	for _, file := range record.Attachments {
		if strings.HasPrefix(strings.ToLower(file.ContentType), "image/") {
			comment.Images++
		} else {
			comment.Files++
		}
		if address := mcpCommentFileAddress(reader.app, reader.authority, record.ID, file); address != "" {
			comment.FileURLs = append(comment.FileURLs, address)
		}
	}
	return comment, nil
}

// notionMCPRichTextFromSpan is a comment's span as Notion's rich text, and
// its words. A page or database mention reads with its title now, and leads,
// for an answer with an address of its own (issuer, nil for none), where a
// block's mention does (notionBlockMention); a person mention's person reads
// as the reader reads them (notionPersonObject).
func notionMCPRichTextFromSpan(
	app core.App, workspaceID string, span mcpCommentSpan, closed searchMentionCloser, issuer *notionBlockFileCapabilityIssuer,
	reader notionOAuthBearerIdentity,
) (map[string]any, string, error) {
	// A page or database the reader can't open is an unnamed page mention
	// by its ID, as in a block's rich text (notionUnnamedMention).
	if searchMentionClosed(closed, span.Mention, span.PageID, span.DatabaseID) {
		target, err := notionPagePublicID(app, span.PageID)
		if err != nil {
			target, err = span.PageID, nil
		}
		if span.Mention == "database" {
			target, err = notionDatabaseMentionPublicID(app, workspaceID, span.DatabaseID)
		}
		if err != nil {
			return nil, "", err
		}
		mention := notionUnnamedMention(target)
		delete(mention, notionMentionUnnamedKey)
		item := map[string]any{"type": "mention", "mention": mention, "href": issuer.mentionAddress(app, target)}
		notionUnnamedMentionItem(item)
		return item, notionUnresolvedMentionTitle, nil
	}
	// A person mention reads with the person's name now (person_mention_names.go).
	if span.Mention == "person" {
		names, err := personMentionNames(app, workspaceID, []string{span.UserID})
		if err != nil {
			return nil, "", err
		}
		span.Text = personMentionText(names, span.UserID, span.Text)
	}
	annotations := map[string]any{
		"bold": span.Bold, "italic": span.Italic, "strikethrough": span.Strikethrough,
		"underline": span.Underline, "code": span.Code, "color": mcpCommentColor(span.Color),
	}
	plain := span.Text
	base := map[string]any{
		"annotations": annotations, "plain_text": plain, "href": nil,
	}
	// A bot reads as a block's mention of one does (notionBlockMention), unless kept as no one's.
	if bot, found := notionMentionBotFor(app, reader, workspaceID, span.UserID); span.Mention == "person" && found &&
		!notionUnknownPersonMention(span.UserID, span.Text) {
		var name any
		if bot.Name != "" {
			name = bot.Name
		}
		base["type"], base["plain_text"] = "mention", "@"+bot.Name
		base["mention"] = map[string]any{"type": "user", "user": map[string]any{
			"object": "user", "id": bot.ID, "name": name, "avatar_url": nil, "type": "bot", "bot": map[string]any{},
		}}
		return base, "@" + bot.Name, nil
	}
	switch {
	case span.Mention == "person":
		userID, err := notionCompatibleUserID(span.UserID)
		// Another token's bot reads by its ID alone, as in a block's text,
		// as does one kept as no one's (notionCommentMarkdownUnknownPerson).
		_, tokenBot := notionTokenMentionBot(app, workspaceID, span.UserID)
		if _, kept := normalizeNotionUUIDRequest(span.UserID); err != nil && (tokenBot || kept) {
			userID, err = strings.TrimSpace(span.UserID), nil
		}
		if err != nil {
			return nil, "", err
		}
		user, inWorkspace, err := loadNotionMentionUser(app, workspaceID, span.UserID)
		if err != nil {
			return nil, "", err
		}
		_, emails := notionIdentityReadsPeople(app, reader)
		person, err := notionPersonObject(reader, emails, user, inWorkspace, userID)
		if err != nil {
			return nil, "", err
		}
		base["type"] = "mention"
		base["mention"] = map[string]any{"type": "user", "user": person}
	case span.Mention == "page":
		pageID, err := notionPagePublicID(app, span.PageID)
		if err != nil {
			// A database row page mention carries the row's stored identity.
			pageID, err = ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, workspaceID, span.PageID)
		}
		if err != nil {
			return nil, "", err
		}
		base["type"] = "mention"
		base["mention"] = map[string]any{
			"type": "page", "page": map[string]any{"id": pageID},
		}
		base["href"] = issuer.mentionAddress(app, pageID)
		if span.BlockID != "" {
			// One of a block reads with the block's words now, leading to
			// the page at the block, as a block's text reads it
			// (notionBlockMentionReading).
			if block, there := pageMentionAnchorBlock(app, span.PageID, span.BlockID); there {
				plain = pageMentionBlockWords(app, block,
					pageMentionBlockSayerClosing(app, func() searchMentionCloser { return closed }, workspaceID))
			}
			if plain == "" {
				plain = notionUnresolvedMentionTitle
			}
			base["plain_text"] = plain
			if address, ok := base["href"].(string); ok {
				base["href"] = address + "#" + notionBlockAnchorHex(app, workspaceID, span.BlockID)
			}
			break
		}
		if title, _, there := pageMentionTargetNow(app, span.PageID); there {
			if plain = title; plain == "" {
				plain = notionUntitledPageMentionTitle
			}
			base["plain_text"] = plain
		}
	case span.Mention == "database":
		databaseID, err := notionDatabaseMentionPublicID(app, workspaceID, span.DatabaseID)
		if err != nil {
			return nil, "", err
		}
		base["type"] = "mention"
		base["mention"] = map[string]any{
			"type": "database", "database": map[string]any{"id": databaseID},
		}
		base["href"] = issuer.mentionAddress(app, databaseID)
		if title, found, err := databaseContainerTitle(app, span.DatabaseID); err != nil {
			return nil, "", err
		} else if found && title != "" {
			plain = title
			base["plain_text"] = plain
		}
	case span.Mention == "custom_emoji":
		base["type"] = "mention"
		base["mention"] = map[string]any{
			"type": "custom_emoji", "custom_emoji": map[string]any{"id": span.CustomEmojiID},
		}
	case span.Mention == "date":
		base["type"] = "mention"
		base["mention"] = map[string]any{
			"type": "date", "date": map[string]any{
				"start": span.Date, "end": optionalMCPAny(span.DateEnd),
				"time_zone": optionalMCPAny(span.DateTimeZone),
			},
		}
	case span.Equation != "":
		base["type"] = "equation"
		base["equation"] = map[string]any{"expression": span.Equation}
		base["plain_text"] = span.Equation
		plain = span.Equation
	default:
		base["type"] = "text"
		link := any(nil)
		if span.Link != "" {
			// Read out as a block's text link is (notionAPITextLink).
			origin := ""
			if issuer != nil && issuer.initialize(app) == nil {
				origin = issuer.origin
			}
			address := notionAPITextLink(app, workspaceID, "", span.Link, origin)
			link = map[string]any{"url": address}
			base["href"] = address
		}
		base["text"] = map[string]any{"content": span.Text, "link": link}
	}
	return base, plain, nil
}

func optionalMCPAny(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mcpCommentColor(value string) string {
	if value == "" {
		return "default"
	}
	return value
}

// mcpDiscussionElement writes a discussion with its first shown comments:
// get-comments' (listing) counts all its comments and names each author's
// email, the reader's own comments and each comment's files' download
// addresses (attachment-urls, space-separated); fetch's doesn't. Both count
// each comment's attached files, its pictures apart, as Notion's do (seen
// 2026-11-11: file-attached-count, image-attached-count).
func mcpDiscussionElement(discussion notionMCPDiscussion, shown int, listing bool) []string {
	shown = min(shown, len(discussion.Comments))
	counts := fmt.Sprintf(`comment-count="%d"`, shown)
	if listing {
		counts += fmt.Sprintf(` total-comment-count="%d"`, len(discussion.Comments))
	}
	where := mcpDiscussionWhere(discussion)
	if discussion.Kind != "" {
		where += ` kind="` + discussion.Kind + `"`
	}
	lines := []string{fmt.Sprintf(`<discussion id="%s" %s resolved="%t" type="comment" %s>`,
		mcpFetchAttribute(discussion.DiscussionID), counts, discussion.Resolved, where)}
	for _, comment := range discussion.Comments[:shown] {
		user, mine := comment.UserURL, ""
		if listing && comment.Email != "" {
			user += "/" + comment.Email
		}
		if listing && comment.ByYou {
			mine = ` by-you="true"`
		}
		if comment.Files > 0 {
			mine += fmt.Sprintf(` file-attached-count="%d"`, comment.Files)
		}
		if comment.Images > 0 {
			mine += fmt.Sprintf(` image-attached-count="%d"`, comment.Images)
		}
		mine += mcpCommentListingAttributes(comment, listing)
		lines = append(lines, fmt.Sprintf(`<comment id="%s" url="%s" user-url="%s" datetime="%s"%s>%s</comment>`,
			mcpFetchAttribute(comment.ID), mcpFetchAttribute(comment.URL), mcpFetchAttribute(user),
			mcpFetchAttribute(comment.CreatedTime), mine, comment.Body))
	}
	return append(lines, "</discussion>")
}
