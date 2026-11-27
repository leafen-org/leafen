package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A page's copy kept in its reader's browser holds the page's page and
// database mentions as the reader's socket last sent them
// (collaboration_reader_mentions.go): a title, or the marks of one closed to
// them or missing. The socket sends a document's text once, so the copy
// keeps those after the target opens, closes or goes. The app asks how the
// copy's mentions stand to its reader now, and opens the page from the
// server's copy when one differs (collaborationCopyMentions.ts). An open
// screen asks the same of the mentions it shows the workspace's list lacks
// as they come on screen and at each change of the reader's access — a
// share, a group, a teamspace or a place taken from them changes no page
// mentioning what it closes — and draws each closed or gone so
// (useMentionReach.ts). A mention's target is any page or database: the
// answer is the reader's, whichever page they ask on.

const maxPageCopyMentions = 256

type pageCopyMention struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// Reach is "open", "closed" or "missing", as the reader's socket reads it.
	Reach string `json:"reach,omitempty"`
}

func registerPageCopyMentionRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/pages/{id}/copy-mentions", readPageCopyMentions)
}

func readPageCopyMentions(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := struct {
		Mentions []pageCopyMention `json:"mentions"`
	}{}
	if err := event.BindBody(&input); err != nil || len(input.Mentions) == 0 || len(input.Mentions) > maxPageCopyMentions {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the mentions.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	if _, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	} else if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeGraphReadFailed,
			"Couldn't read the mentions.", "page_copy_mentions", "authorize", errorCodeGraphReadFailed, err)
	}
	targets := make([]mentionTarget, 0, len(input.Mentions))
	for index, mention := range input.Mentions {
		mention.ID = strings.TrimSpace(mention.ID)
		if mention.ID == "" || len(mention.ID) > maxWorkspacePermissionIdentifierLength ||
			(mention.Kind != "page" && mention.Kind != "database") {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the mentions.")
		}
		input.Mentions[index].ID = mention.ID
		targets = append(targets, mentionTarget{mention: mention.Kind, id: mention.ID})
	}
	// The pages they name are worked out together (mention_closer_learning.go).
	closes := pageReader(user.Id).closesKnowing(event.App, targets)
	mentions := make([]pageCopyMention, 0, len(input.Mentions))
	for _, mention := range input.Mentions {
		pageTarget, databaseTarget := mention.ID, ""
		if mention.Kind == "database" {
			pageTarget, databaseTarget = "", mention.ID
		}
		switch searchMentionReach(closes, mention.Kind, pageTarget, databaseTarget) {
		case mentionMissing:
			mention.Reach = "missing"
		case mentionClosed:
			mention.Reach = "closed"
		default:
			mention.Reach = "open"
		}
		mentions = append(mentions, mention)
	}
	return event.JSON(http.StatusOK, map[string]any{"mentions": mentions})
}
