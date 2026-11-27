package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// Every public API write points its links and its page, database and data
// source mentions here by the rule Markdown's follow (notion_address_links_here.go),
// JSON and Markdown alike: before 2026-11-21 a new page's JSON content passed
// the rule while a block appended (PATCH /v1/blocks/{id}/children) or changed
// (PATCH /v1/blocks/{id}) through JSON kept a Notion ID and address as
// written, so the same content named a page a Notion move brought or not by
// the way it was sent. A link or mention of nothing here stays as written —
// a JSON mention, holding no address, as the "Untitled" one Notion keeps.

// prepareNotionWriteTargetsInput readies what a public API write to a page's
// body names before its mentions are checked and named (prepareNotionMentionsInput):
// its file uploads (prepareNotionBlockFileUploadsInput), then its links and
// mentions in its text, table cells and captions (notionWriteTargetsHere).
func prepareNotionWriteTargetsInput(
	app core.App, identity notionOAuthBearerIdentity, current pageAuthorization, input graphMutationInput,
) (graphMutationInput, error) {
	input, err := prepareNotionBlockFileUploadsInput(app, identity, input)
	if err != nil {
		return graphMutationInput{}, err
	}
	return input, notionWriteTargetsHere(app, current.Workspace.WorkspaceID, input.TextUpdates, input.Operations)
}

// notionSpansHere points a public API write's name, title or value's links
// and mentions here before prepare checks and names them.
func notionSpansHere(app core.App, current pageAuthorization, prepare func([]graphTextSpan) error) func([]graphTextSpan) error {
	here := newNotionAddressLinksHere(app, current.Workspace.WorkspaceID)
	return func(spans []graphTextSpan) error {
		if _, err := here.spans(spans); err != nil {
			return err
		}
		return prepare(spans)
	}
}

// notionCommentGraphSpansHere reads a comment's JSON rich text
// (notionCommentGraphSpans) with its links and mentions pointed here, as a
// comment's Markdown's are (notionCommentMarkdownSpans).
func notionCommentGraphSpansHere(app core.App, page pageAuthorization, items []json.RawMessage) ([]graphTextSpan, error) {
	spans, err := notionCommentGraphSpans(items)
	if err != nil {
		return nil, err
	}
	_, err = newNotionAddressLinksHere(app, page.Workspace.WorkspaceID).spans(spans)
	return spans, err
}
