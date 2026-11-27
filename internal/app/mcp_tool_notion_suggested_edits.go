package app

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's suggested edits (its tools' descriptions, 2026-11-13; a workspace
// on Business or above runs them, so their answers weren't seen): one change
// proposed on exact text copied from fetch — replaced, or new text put before
// or after it — read with its before and after, and accepted or rejected;
// old_text is found as a comment's selection is (findMCPSuggestionPlace), and
// new_text is Notion Markdown (mcp_tool_notion_suggested_edit_text.go).
// Leafen's suggestions are its screen's (page_suggestions.go): words deleted
// or inserted in a block's text, a line break, new blocks, each a thread of
// its own, so a replacement is a deletion and an insertion, as typing over
// words in suggesting mode makes them (mcp_tool_notion_suggested_edit_steps.go);
// its address names every thread, and reading or settling it reads or
// settles them all. The suggestions get-comments lists are read and settled
// one by one, as the screen shows them.

const notionMCPSuggestedPair = "with"

type notionMCPCreateSuggestedEditInput struct {
	PageID       string `json:"page_id" jsonschema:"The page's ID or address"`
	OldText      string `json:"old_text" jsonschema:"Exact text copied from the page's Markdown as fetch gives it, marks and block markers in it or its words alone, found once in the page and within one block"`
	NewText      string `json:"new_text" jsonschema:"Proposed Notion Markdown, as fetch writes it: with replace, what replaces old_text (empty to suggest deleting it); with insert_before or insert_after, only what goes there. Its marks, links and mentions are the words' formatting, and <br> a line break in the block. Where old_text starts the block's line, new_text starts with the same marker or none, or the block's own tag as fetch writes it: a suggestion changes a block's words, not its kind or look. Its line breaks suggest new blocks, one a line with a blank line between them or none, a block's tag over lines as fetch writes one (<p>, <callout>) one line, the block's words after the edit going on the last; a callout's words go on the first line inside its tag, as fetch writes them. A suggested block takes its kind's own look, as suggesting mode makes one: a color, a callout's icon other than 💡 or a list's letters are refused"`
	Mode         string `json:"mode,omitempty" jsonschema:"replace (the default), insert_before or insert_after"`
	PropertyName string `json:"property_name,omitempty" jsonschema:"Leafen suggests edits in a page's body: leave it out"`
	Comment      string `json:"comment,omitempty" jsonschema:"Optional inline Markdown comment that starts the suggestion's thread"`
}

type notionMCPSuggestedEditInput struct {
	SuggestedEditURL string `json:"suggested_edit_url" jsonschema:"The suggested edit's discussion:// address from notion-create-suggested-edit or notion-get-comments"`
}

type notionMCPResolveSuggestedEditInput struct {
	SuggestedEditURL string `json:"suggested_edit_url" jsonschema:"The suggested edit's discussion:// address from notion-create-suggested-edit or notion-get-comments"`
	Action           string `json:"action" jsonschema:"accept or reject"`
}

type notionMCPSuggestedEditOutput struct {
	SuggestedEditURL string   `json:"suggested_edit_url"`
	Status           string   `json:"status"`
	PageURL          string   `json:"page_url"`
	Author           string   `json:"author,omitempty"`
	Before           string   `json:"before" jsonschema:"The words of the block it edits as they stand, a line break inside the block written <br /> as fetch writes it"`
	After            string   `json:"after" jsonschema:"Those words with the edit in, a line a block, a line break inside a block written <br /> as fetch writes it; a code block's lines as they are"`
	AllowedActions   []string `json:"allowed_actions"`
}

func registerMCPSuggestedEditTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-create-suggested-edit", Title: "Suggest an edit in Leafen",
		Description: "Propose one precise, reviewable change to a page's text without changing it, as suggesting mode does. Copy old_text exactly from fetch: it must be found once in the page, within one block. new_text is Notion Markdown, and its lines after the first are new blocks. Use one suggestion per change, and notion-get-suggested-edit to read it before it is settled.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPCreateSuggestedEditInput) (*mcp.CallToolResult, notionMCPSuggestedEditOutput, error) {
		return createMCPSuggestedEdit(ctx, request, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-suggested-edit", Title: "Read a Leafen suggested edit",
		Description: "Read a suggested edit's text before and after it — its blocks' words, a line each after it, a line break inside a block written <br /> as fetch writes it — whether it is pending, accepted, rejected or withdrawn, and the actions the person may take on it now. Call this before accepting or rejecting it.",
		Annotations: readOnly,
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPSuggestedEditInput) (*mcp.CallToolResult, notionMCPSuggestedEditOutput, error) {
		return getMCPSuggestedEdit(ctx, app, input.SuggestedEditURL)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-resolve-suggested-edit", Title: "Accept or reject a Leafen suggested edit",
		Description: "Accept or reject one pending suggested edit by its exact address: read it with notion-get-suggested-edit first, and call this only once the person chose accept or reject. Someone who can edit the page accepts or rejects; the one who suggested it may reject it.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPResolveSuggestedEditInput) (*mcp.CallToolResult, notionMCPSuggestedEditOutput, error) {
		return resolveMCPSuggestedEdit(ctx, request, app, input)
	})
}

// mcpSuggestionPage is a page a suggestion tool works on.
func mcpSuggestionPage(app core.App, authority mcpAuthority, raw string) (string, error) {
	pageID := mcpLocalID(app, raw)
	if _, _, readable := mcpListedPage(app, authority, pageID); !readable {
		return "", mcpNotFound("page", raw)
	}
	return pageID, nil
}

// mcpSuggestionPlace is where old_text is: its block, the block's type, its
// text and runs, and the text's place in it, in UTF-16 units as the screen
// counts them; with what it holds at its edges in the page's Markdown — the
// part of the line's marker it holds (Held), whether it starts the line
// (LineStart), and whether it starts or ends on markup (Opens, Closes).
type mcpSuggestionPlace struct {
	BlockID, Type, Text string
	Runs                []suggestionTextRun
	Offset, Length      int
	Held                string
	LineStart           bool
	Opens, Closes       bool
}

// findMCPSuggestionPlace finds old_text as a comment's selection is found
// (mcp_tool_comment_selection_markdown.go), as exact text: once in the
// page's Markdown as fetch writes it — Notion's tool takes "exact text
// copied from the accepted Notion Markdown returned by fetch" (its
// description, 2026-11-22) — so "<b>에코</b>" or "## 구역 둘" copied from fetch
// names the words it writes, and words standing twice among the page's
// words but once in its Markdown name one place; else once among the
// page's words. Its words are a block's that takes suggestions.
func findMCPSuggestionPlace(app core.App, authority mcpAuthority, pageID, oldText string) (mcpSuggestionPlace, pageAuthorization, error) {
	current, err := authorizePage(app, authority.PrincipalID, pageID, permissionWorkspaceRead)
	if err != nil {
		return mcpSuggestionPlace{}, current, failedBecause(errMCPToolFailed, err)
	}
	document, err := loadPageTextDocument(app, pageID)
	if err != nil {
		return mcpSuggestionPlace{}, current, failedBecause(errMCPToolFailed, err)
	}
	texts, err := readMCPSelectionTexts(app, pageID, document, mcpReaderCloses(app, authority), mcpSelectionPageOf(authority, current.Workspace))
	if err != nil {
		return mcpSuggestionPlace{}, current, err
	}
	query, _ := readMCPExactText(oldText)
	found := texts.find(texts.markdown.placeText(oldText), query)
	_, takes := suggestionBlockTypes[texts.markdown.types[found.blockID]]
	switch {
	case found.found == 0:
		return mcpSuggestionPlace{}, current, mcpInvalid("old_text isn't in the page's Markdown as fetch gives it: copy it exactly from fetch, within one block")
	case found.found > 1:
		return mcpSuggestionPlace{}, current, mcpInvalid("old_text is in the page %d times: give more of its words so it names one place", found.found)
	case found.blockID != "" && !takes:
		return mcpSuggestionPlace{}, current, mcpInvalid("old_text is in a block whose text takes no suggested edits: a suggestion changes a paragraph's, heading's, list item's, to-do's, toggle's, quote's, callout's or code block's words")
	case found.past:
		return mcpSuggestionPlace{}, current, mcpInvalid("old_text runs past its block's text: a suggested edit stays within one block; suggest each block's change on its own")
	case found.marked == nil || found.marked.end <= found.marked.start:
		return mcpSuggestionPlace{}, current, mcpInvalid("old_text holds none of a block's words, only the Markdown around them: copy the words to change from fetch")
	}
	text := document.GetText(ygoBlockTextRootPrefix + found.blockID)
	place := mcpSuggestionPlace{BlockID: found.blockID, Type: texts.markdown.types[found.blockID], Text: text.ToString(),
		Runs: suggestionTextRuns(text), Offset: found.marked.start, Length: found.marked.end - found.marked.start,
		Held: found.edges.held, LineStart: found.edges.lineStart, Opens: found.edges.opens, Closes: found.edges.closes}
	// Found among the words, old_text starts a line with no marker at its words' start.
	if !found.edges.inMarkdown {
		place.LineStart = place.Offset == 0 && strings.TrimSpace(texts.markdown.marker(found.blockID)) == ""
	}
	return place, current, nil
}

func createMCPSuggestedEdit(ctx context.Context, request *mcp.CallToolRequest, app core.App, input notionMCPCreateSuggestedEditInput) (*mcp.CallToolResult, notionMCPSuggestedEditOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	mode := strings.TrimSpace(input.Mode)
	switch {
	case strings.TrimSpace(input.PropertyName) != "":
		return nil, notionMCPSuggestedEditOutput{}, mcpInvalid("Leafen takes suggested edits in a page's body text, not its title or properties: to change those, use notion-update-page")
	case strings.TrimSpace(input.OldText) == "":
		return nil, notionMCPSuggestedEditOutput{}, mcpInvalid("old_text is exact words from the page, not empty")
	case mode != "" && mode != "replace" && mode != "insert_before" && mode != "insert_after":
		return nil, notionMCPSuggestedEditOutput{}, mcpInvalid("mode is replace, insert_before or insert_after")
	case strings.ContainsAny(input.OldText, "\r\n"):
		return nil, notionMCPSuggestedEditOutput{}, mcpInvalid("old_text stays within one block's line, with no line breaks: suggest each block's change on its own")
	case mode != "" && mode != "replace" && input.NewText == "":
		return nil, notionMCPSuggestedEditOutput{}, mcpInvalid("with %s, new_text is the words to put there", mode)
	}
	pageID, err := mcpSuggestionPage(app, authority, input.PageID)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	place, current, err := findMCPSuggestionPlace(app, authority, pageID, input.OldText)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	text, err := readMCPSuggestedText(app, authority, current, place, mode, input.NewText)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	suggester := &mcpSuggester{ctx: ctx, app: app, pageID: pageID}
	if err := suggester.suggest(place, mode, text); err != nil {
		// What the steps before suggested goes back.
		if len(suggester.threads) > 0 {
			address := mcpSuggestedEditURL(app, suggester.workspaceID, pageID, place.BlockID, suggester.threads)
			if edit, readErr := readMCPSuggestedEdit(app, authority, address); readErr == nil {
				_ = settleMCPSuggestedEdit(ctx, edit, "reject")
			}
		}
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	threads, workspaceID := suggester.threads, suggester.workspaceID
	address := mcpSuggestedEditURL(app, workspaceID, pageID, place.BlockID, threads)
	if strings.TrimSpace(input.Comment) != "" {
		comment := input.Comment
		_, _, err := createMCPComment(ctx, request, app, notionMCPCreateCommentInput{
			PageID: pageID, DiscussionID: mcpDiscussionID(app, workspaceID, pageID, place.BlockID, threads[0]), Markdown: &comment,
		})
		if err != nil {
			return nil, notionMCPSuggestedEditOutput{}, err
		}
	}
	return getMCPSuggestedEdit(ctx, app, address)
}

// parseMCPSuggestedEditURL reads a suggested edit's page, block and threads.
func parseMCPSuggestedEditURL(app core.App, authority mcpAuthority, raw string) (string, string, []string, error) {
	address, pair, _ := strings.Cut(strings.TrimSpace(raw), "?")
	pageID, blockID, rootID, err := parseMCPDiscussionID(app, authority, address)
	if err != nil || rootID == "" {
		return "", "", nil, mcpInvalid("suggested_edit_url is the discussion:// address notion-create-suggested-edit or notion-get-comments gave")
	}
	threads := []string{rootID}
	if values, err := url.ParseQuery(pair); err == nil {
		for _, thread := range values[notionMCPSuggestedPair] {
			if thread != "" && !slices.Contains(threads, thread) {
				threads = append(threads, thread)
			}
		}
	}
	return pageID, blockID, threads, nil
}

// mcpSuggestedEdit reads a suggested edit's threads as they are now.
type mcpSuggestedEdit struct {
	pageID, blockID string
	threads         []commentRecord
	kinds           map[string]string
	states          map[string]string
}

func readMCPSuggestedEdit(app core.App, authority mcpAuthority, raw string) (mcpSuggestedEdit, error) {
	pageID, blockID, threads, err := parseMCPSuggestedEditURL(app, authority, raw)
	if err != nil {
		return mcpSuggestedEdit{}, err
	}
	if pageID, err = mcpSuggestionPage(app, authority, pageID); err != nil {
		return mcpSuggestedEdit{}, err
	}
	target, err := authorizeMCPCommentTarget(app, authority, pageID, permissionWorkspaceRead)
	if err != nil {
		return mcpSuggestedEdit{}, err
	}
	records, err := listAllCommentRecordsForTarget(app, target.Authorization.Target,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeMCPCommentTargetCurrent(txApp, authority, target.Authorization.Target, permissionWorkspaceRead)
		})
	if err != nil {
		return mcpSuggestedEdit{}, mcpCommentError(err)
	}
	edit := mcpSuggestedEdit{pageID: pageID, blockID: blockID, kinds: map[string]string{}, states: map[string]string{}}
	for _, id := range threads {
		index := slices.IndexFunc(records, func(record commentRecord) bool { return record.ID == id && record.ParentID == "" })
		body := suggestionThreadBody{}
		if index < 0 || json.Unmarshal(records[index].Body, &body) != nil || body.Suggestion.Kind == "" {
			return mcpSuggestedEdit{}, mcpDenied("no suggested edit has the address %s: notion-get-comments lists the page's", raw)
		}
		edit.threads = append(edit.threads, records[index])
		edit.kinds[id], edit.states[id] = body.Suggestion.Kind, body.Suggestion.State
		if edit.blockID == "" {
			edit.blockID = records[index].BlockID
		}
	}
	return edit, nil
}

func getMCPSuggestedEdit(ctx context.Context, app core.App, raw string) (*mcp.CallToolResult, notionMCPSuggestedEditOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	edit, err := readMCPSuggestedEdit(app, authority, raw)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	output := notionMCPSuggestedEditOutput{SuggestedEditURL: raw, Status: "pending", PageURL: mcpPageURL(authority, edit.pageID), AllowedActions: []string{}}
	if id, err := notionCompatibleUserID(edit.threads[0].AuthorID); err == nil {
		output.Author = "user://" + id
	}
	ids := []string{}
	for _, thread := range edit.threads {
		ids = append(ids, thread.ID)
		if state := edit.states[thread.ID]; state != "" {
			output.Status = state
		}
	}
	document, err := loadPageTextDocument(app, edit.pageID)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, failedBecause(errMCPToolFailed, err)
	}
	blocks, suggested, code, err := mcpSuggestedEditBlocks(app, document, edit)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	// Before: its blocks' words as they stand, the blocks a break made one
	// with the block before it; after: with this edit's insertions in and its
	// deletions out, a line a block — each block's line breaks written as
	// fetch writes them (mcpSuggestedEditWords).
	before, after, inCode := strings.Builder{}, []string{}, false
	for _, blockID := range blocks {
		inCode = inCode || code[blockID]
		line := strings.Builder{}
		for _, span := range graphTextSpansFromYText(document.GetText(ygoBlockTextRootPrefix + blockID)) {
			mine := slices.Contains(ids, span.SuggestionID)
			switch span.Suggestion {
			case textSuggestionInsert:
				if mine {
					line.WriteString(span.Text)
				}
			case textSuggestionDelete:
				before.WriteString(span.Text)
				if !mine {
					line.WriteString(span.Text)
				}
			default:
				if !suggested[blockID] {
					before.WriteString(span.Text)
				}
				line.WriteString(span.Text)
			}
		}
		after = append(after, mcpSuggestedEditWords(line.String(), code[blockID]))
	}
	output.Before, output.After = mcpSuggestedEditWords(before.String(), inCode), strings.Join(after, "\n")
	// Settled, its blocks stand as they are.
	if output.Status != "pending" {
		output.Before = output.After
	}
	if output.Status == "pending" {
		if _, err := authorizePageBody(app, authority.PrincipalID, edit.pageID, permissionWorkspaceWrite); err == nil {
			output.AllowedActions = []string{"accept", "reject"}
		} else if edit.threads[0].AuthorID == authority.PrincipalID {
			output.AllowedActions = []string{"reject"}
		}
	}
	return nil, output, nil
}

func resolveMCPSuggestedEdit(ctx context.Context, request *mcp.CallToolRequest, app core.App, input notionMCPResolveSuggestedEditInput) (*mcp.CallToolResult, notionMCPSuggestedEditOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	if input.Action != "accept" && input.Action != "reject" {
		return nil, notionMCPSuggestedEditOutput{}, mcpInvalid("action is accept or reject")
	}
	edit, err := readMCPSuggestedEdit(app, authority, input.SuggestedEditURL)
	if err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	for _, thread := range edit.threads {
		if edit.states[thread.ID] != "" || thread.Resolved {
			return nil, notionMCPSuggestedEditOutput{}, mcpInvalid("the suggested edit was already accepted, rejected or withdrawn")
		}
	}
	if err := settleMCPSuggestedEdit(ctx, edit, input.Action); err != nil {
		return nil, notionMCPSuggestedEditOutput{}, err
	}
	return getMCPSuggestedEdit(ctx, app, input.SuggestedEditURL)
}

// settleMCPSuggestedEdit accepts or rejects a suggested edit's threads, the
// last made first: words suggested in where a break moved them settle before
// the break that would take them back.
func settleMCPSuggestedEdit(ctx context.Context, edit mcpSuggestedEdit, action string) error {
	for index := len(edit.threads) - 1; index >= 0; index-- {
		thread := edit.threads[index]
		result, err := mcpNativeCall(ctx, "POST", "/pages/"+edit.pageID+"/suggestions/"+url.PathEscape(thread.ID)+"/resolve", "",
			suggestionResolveInput{RequestID: "mcp-settle:" + newDiagnosticReference(), Decision: action, ExpectedRevision: thread.Revision})
		if err != nil {
			return err
		}
		if err := mcpNativeRefusal(result); err != nil {
			return err
		}
	}
	return nil
}

// mcpSuggestedLiteralBreak is a <br> tag's shape at the start of words.
var mcpSuggestedLiteralBreak = regexp.MustCompile(`^(?i)<br\s*/?>`)

// mcpSuggestedEditWords writes a block's words in a suggested edit's before
// and after as fetch writes a line break inside a block, <br /> — the after
// a line a block, so a break inside one reads apart from the next block —
// and words fetch would read as a break, "<br />" typed as words, as it
// escapes them, "\<br />", a backslash just before either doubled as
// fetch doubles it; other words as they are. A code block's words stay as
// they are, lines and all, as fetch's fence holds them: an edit of its
// words is that block alone.
func mcpSuggestedEditWords(words string, code bool) string {
	if code || !strings.Contains(words, "\n") && !strings.Contains(strings.ToLower(words), "<br") {
		return words
	}
	breaks := func(at int) bool {
		return at < len(words) && (words[at] == '\n' || mcpSuggestedLiteralBreak.MatchString(words[at:]))
	}
	var out strings.Builder
	for index := 0; index < len(words); {
		switch {
		case words[index] == '\\':
			run := index + len(words[index:]) - len(strings.TrimLeft(words[index:], `\`))
			out.WriteString(words[index:run])
			if breaks(run) {
				out.WriteString(words[index:run])
			}
			index = run
		case words[index] == '\n':
			out.WriteString("<br />")
			index++
		case breaks(index):
			out.WriteByte('\\')
			out.WriteByte('<')
			index++
		default:
			out.WriteByte(words[index])
			index++
		}
	}
	return out.String()
}
