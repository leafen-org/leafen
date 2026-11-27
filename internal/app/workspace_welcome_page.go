package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A workspace that starts empty, rather than from an import, opens on a
// short welcome page, as Notion's Getting Started page does, in the language
// the request names or else the account's; a client may send its own. The
// person may keep, change or delete it.

const (
	maxWorkspaceWelcomeTitleRunes    = 200
	maxWorkspaceWelcomeMarkdownBytes = 16 << 10
	workspaceWelcomeIcon             = "👋"
)

func normalizeWorkspaceWelcomePage(input *workspaceWelcomePageInput) (*workspaceWelcomePageInput, bool) {
	if input == nil {
		return nil, true
	}
	title, markdown := strings.TrimSpace(input.Title), strings.TrimSpace(input.Markdown)
	if title == "" || len([]rune(title)) > maxWorkspaceWelcomeTitleRunes ||
		len(markdown) > maxWorkspaceWelcomeMarkdownBytes {
		return nil, false
	}
	if _, err := notionMarkdownReplacementPlan(markdown, nil, false); err != nil {
		return nil, false
	}
	return &workspaceWelcomePageInput{Title: title, Markdown: markdown}, true
}

// workspaceWelcomeTextFor is the welcome page in the language tag, or in the
// language the server names things in where the catalog doesn't have it.
func workspaceWelcomeTextFor(tag, language string) *workspaceWelcomePageInput {
	text, found := workspaceWelcomeTexts[strings.TrimSpace(tag)]
	if !found {
		text = workspaceWelcomeTexts[language]
	}
	return &text
}

// writeWorkspaceWelcomePage writes the welcome body into a new workspace's
// starter page. A retried creation replays the same mutation instead of
// writing the body twice.
func writeWorkspaceWelcomePage(
	ctx context.Context,
	ygo *pocketBaseYgoPersistence,
	ownerID, pageID string,
	welcome *workspaceWelcomePageInput,
) error {
	if ygo == nil || welcome == nil || pageID == "" || welcome.Markdown == "" {
		return nil
	}
	plan, err := notionMarkdownReplacementPlan(welcome.Markdown, nil, false)
	if err != nil {
		return err
	}
	operations, textUpdates := plan.Operations, plan.TextUpdates
	if workspaceWelcomeReadsFromRight(welcome) {
		if err := setWelcomeLinesFromRight(operations); err != nil {
			return err
		}
	}
	// A body written with its text is one change of the page as it stands.
	page, err := ygo.app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	if err := resolveNotionMarkdownCustomEmojis(ygo.app, page.GetString("workspace"), textUpdates); err != nil {
		return err
	}
	if err := resolveNotionMarkdownCalloutIcons(ygo.app, page.GetString("workspace"), operations); err != nil {
		return err
	}
	var graphRevision int
	if err := ygo.app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]), 0) FROM {{leafen_graph_mutations}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"owner": page.GetString("owner"), "pageId": pageID}).Row(&graphRevision); err != nil {
		return err
	}
	pageCursor, pageTitle := page.GetInt("cursor"), page.GetString("title")
	digest := sha256.Sum256([]byte("workspace-welcome\x00" + pageID + "\x00" + welcome.Markdown))
	_, err = ygo.applyContentGraphMutationContext(ctx, ownerID, pageID, graphMutationInput{
		MutationID: "workspace-welcome:" + pageID, Operations: operations, TextUpdates: textUpdates,
		BasePageCursor: &pageCursor, BaseGraphRevision: &graphRevision, PageTitle: &pageTitle,
	}, contentGraphMutationHooks{
		SuppressAutomationEvents: true, SuppressNotifications: true,
		ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
			return hex.EncodeToString(digest[:]), nil
		},
	})
	return err
}
