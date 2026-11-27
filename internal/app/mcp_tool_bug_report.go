package app

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_bug_report drafts a report of a problem in Leafen itself for the
// repository's bug form, as the app's Report issue opens it
// (bug_report_form.go): the six fields, the server's version, system and
// install filled in, the new-issue link with the draft in it and the issues
// already there with the same words. It posts nothing: the agent shows the
// person the draft and posts it, or hands them the link, only on their yes
// (docs/bug-reports.md, user 2026-11-20).

type leafenMCPBugReportInput struct {
	Title   string `json:"title,omitempty" jsonschema:"One line naming the symptom, as someone searching the issues for it would put it."`
	Data    string `json:"data,omitempty" jsonschema:"Synthetic data: made-up content that shows the problem, or the steps that make it in a new workspace. When only real data shows it, its counts, kinds and structure, never its content."`
	Steps   string `json:"steps,omitempty" jsonschema:"Numbered steps from a known state (a new workspace, or the data above just imported), one action each."`
	Result  string `json:"result,omitempty" jsonschema:"What was expected and what happened instead: the exact message, the error's reference (LF-…) when one shows, and whether it happens every time."`
	Fix     string `json:"fix,omitempty" jsonschema:"The fix you think of, in words: where it goes wrong and what should happen instead. No code, patches or diffs."`
	Details string `json:"details,omitempty" jsonschema:"Optional: the error's reference, logs or what the screen showed, with personal data and secrets taken out."`
	Client  string `json:"client,omitempty" jsonschema:"Where the person saw it when not through you: the browser or the phone app, with its version."`
}

type leafenMCPBugReportOutput struct {
	Title   string `json:"title"`
	Version string `json:"version"`
	System  string `json:"system"`
	// The issue's body as the form makes it, its headings kept, for posting.
	Body string `json:"body"`
	// The form's required fields still empty.
	Missing []string `json:"missing,omitempty"`
	// What looks like the person's own data, to replace with made-up data.
	Personal []string `json:"personal,omitempty"`
	// The form with the draft filled in; answers that didn't fit are named.
	NewIssueURL string   `json:"newIssueUrl"`
	NotInLink   []string `json:"notInLink,omitempty"`
	// Issues with the same words, open ones first, and the search itself.
	Similar       []bugReportIssue `json:"similar"`
	SimilarSearch string           `json:"similarSearch"`
	SimilarNote   string           `json:"similarNote,omitempty"`
	Repository    string           `json:"repository"`
	Security      string           `json:"security"`
	Next          string           `json:"next"`
}

func registerMCPBugReportTool(server *mcp.Server, app core.App) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &[]bool{true}[0]}
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_bug_report", Title: "Draft a bug report about Leafen",
		Description: "Draft a report of a problem in Leafen itself — a Leafen tool failing or doing other than its description says, or something in Leafen " +
			"the person finds hard to use — for Leafen's public issue tracker. Never for the content of the person's own data, and never a security " +
			"weakness: those go privately (SECURITY.md). Offer it once per problem, after the task the person asked for, and write it with them. " +
			"Made-up data only: never the person's pages, names, addresses, tokens or codes. Returns the finished report in the form's six fields with " +
			"the server's version and system filled in, a link to GitHub's form with it filled in, and existing issues with the same words. It posts " +
			"nothing: show the person the finished report, and only on their yes post it (with your GitHub login) or give them the link.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPBugReportInput) (*mcp.CallToolResult, leafenMCPBugReportOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, leafenMCPBugReportOutput{}, err
		}
		return nil, draftBugReport(ctx, app, authority, input), nil
	})
}

func draftBugReport(ctx context.Context, app core.App, authority mcpAuthority, input leafenMCPBugReportInput) leafenMCPBugReportOutput {
	title := strings.Join(strings.Fields(input.Title), " ")
	answers := map[string]string{
		"version": currentServerBuild(app).Build, "system": bugReportSystem(app, input.Client),
		"data": input.Data, "steps": input.Steps, "result": input.Result, "fix": input.Fix, "details": input.Details,
	}
	draft := leafenMCPBugReportOutput{
		Title: title, Version: answers["version"], System: answers["system"], Body: bugReportBody(answers),
		Repository: bugReportForm.Repository, Security: bugReportForm.Security, SimilarSearch: bugReportSearchPage(title),
	}
	if title == "" {
		draft.Missing = append(draft.Missing, "Title")
	}
	for _, field := range bugReportForm.Fields {
		if field.Required && strings.TrimSpace(answers[field.ID]) == "" {
			draft.Missing = append(draft.Missing, field.Label)
		}
	}
	draft.Personal = bugReportPersonal(app, authority, title, input.Data, input.Steps, input.Result, input.Fix, input.Details, input.Client)
	draft.NewIssueURL, draft.NotInLink = bugReportLink(title, answers)
	draft.Similar, draft.SimilarNote = bugReportSimilar(ctx, title)
	draft.Next = bugReportNext(draft)
	return draft
}

// bugReportNext says what the agent does with the draft.
func bugReportNext(draft leafenMCPBugReportOutput) string {
	steps := []string{}
	if len(draft.Personal) > 0 {
		steps = append(steps, "Replace with made-up data what looks like "+strings.Join(draft.Personal, ", ")+", then call this again.")
	}
	if len(draft.Missing) > 0 {
		steps = append(steps, "Ask the person, one thing at a time, for what is missing: "+strings.Join(draft.Missing, ", ")+".")
	}
	if len(draft.Similar) > 0 {
		steps = append(steps, "Look at the similar issues with the person: if one is the same problem, suggest adding to it (a comment with what is new) instead of a new issue.")
	} else {
		steps = append(steps, "Before a new issue, look through similarSearch for the same symptom.")
	}
	steps = append(steps,
		"Show the person the title and body as they would be posted, in full, and ask whether to post it. Post nothing before their yes.",
		"On their yes: with a GitHub login (gh), run gh issue create --repo "+draft.Repository+" with the title and the body as it is; "+
			"without one, give them newIssueUrl to check and submit themselves.")
	if len(draft.NotInLink) > 0 {
		steps = append(steps, "The link leaves out "+strings.Join(draft.NotInLink, ", ")+": they paste it from the body into the form.")
	}
	steps = append(steps, "If they say no, drop it and don't offer this report again.")
	return strings.Join(steps, " ")
}
