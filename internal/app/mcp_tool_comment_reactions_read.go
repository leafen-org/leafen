package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// get-comments names each comment's reactions, as the comment panel shows
// them under it: each emoji with how many reacted with it, in the order each
// was first used, and the ones the reader reacted with, so an agent sees
// what notion-set-comment-reaction set and can take its own back. Notion's
// get-comments couldn't be read with reactions (its set-comment-reaction
// needs a Business plan, 2026-11-16), so these are Leafen's own attributes.

// attachMCPCommentParts reads each comment's files and reactions, as the app
// shows them, in one query each.
func attachMCPCommentParts(app core.App, records []commentRecord) error {
	if err := attachCommentAttachments(app, records); err != nil {
		return err
	}
	return attachCommentReactions(app, records)
}

// mcpCommentReactions writes a comment's reactions ("👍 2, 🎉 1") and the
// ones the reader reacted with ("👍").
func mcpCommentReactions(reactions []commentReaction, readerID string) (string, string) {
	counts, yours := []string{}, []string{}
	for _, reaction := range reactions {
		if len(reaction.UserIDs) == 0 {
			continue
		}
		counts = append(counts, fmt.Sprintf("%s %d", reaction.Emoji, len(reaction.UserIDs)))
		if slices.Contains(reaction.UserIDs, readerID) {
			yours = append(yours, reaction.Emoji)
		}
	}
	return strings.Join(counts, ", "), strings.Join(yours, ", ")
}

// mcpCommentListingAttributes are what get-comments says of a comment beyond
// Notion's: its files' download addresses (attachment-urls, space-separated),
// which Notion's leaves out, as the person's comment panel downloads them,
// and its reactions.
func mcpCommentListingAttributes(comment notionMCPComment, listing bool) string {
	if !listing {
		return ""
	}
	attributes := ""
	if len(comment.FileURLs) > 0 {
		attributes += ` attachment-urls="` + mcpFetchAttribute(strings.Join(comment.FileURLs, " ")) + `"`
	}
	if comment.Reactions != "" {
		attributes += ` reactions="` + mcpFetchAttribute(comment.Reactions) + `"`
	}
	if comment.YourReactions != "" {
		attributes += ` reacted-by-you="` + mcpFetchAttribute(comment.YourReactions) + `"`
	}
	return attributes
}
