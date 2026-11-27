package app

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The repository's bug form (.github/ISSUE_TEMPLATE/bug_report.yml) as a
// report is made for it: where a new issue is opened, the form's field ids
// and the headings GitHub gives their answers in an issue's body, where the
// issues are searched, and where security weaknesses go instead. One file
// for the app's Report issue (web/src/lib/errorReport.ts) and an agent's
// report (leafen_bug_report) alike.
//
//go:embed bug_report_form.json
var bugReportFormJSON []byte

type bugReportFormField struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

type bugReportFormSpec struct {
	Repository string               `json:"repository"`
	NewIssue   string               `json:"newIssue"`
	Form       string               `json:"form"`
	Issues     string               `json:"issues"`
	Search     string               `json:"search"`
	Guide      string               `json:"guide"`
	Security   string               `json:"security"`
	Fields     []bugReportFormField `json:"fields"`
}

var bugReportForm = func() bugReportFormSpec {
	spec := bugReportFormSpec{}
	if err := json.Unmarshal(bugReportFormJSON, &spec); err != nil {
		panic("bug_report_form.json: " + err.Error())
	}
	return spec
}()

const (
	// What GitHub writes under a heading left empty, as its forms do.
	bugReportNoResponse = "_No response_"
	// A longer address is cut by browsers and GitHub alike; the answers that
	// don't fit stay out of it, to be pasted from the body.
	bugReportLinkLimit = 7500
	// How long the search for the same symptom may take before the answer
	// goes without it.
	bugReportSearchLimit = 6 * time.Second
	bugReportSimilarMax  = 5
)

// bugReportBody is an issue's body as the form makes it: each field's
// heading and its answer.
func bugReportBody(answers map[string]string) string {
	parts := []string{}
	for _, field := range bugReportForm.Fields {
		answer := strings.TrimSpace(answers[field.ID])
		if answer == "" {
			answer = bugReportNoResponse
		}
		parts = append(parts, "### "+field.Label+"\n\n"+answer)
	}
	return strings.Join(parts, "\n\n")
}

// bugReportLink is the form's new-issue address with the title and the
// answers filled in by their field ids; nothing is sent until the person
// submits the form. Answers that would make it too long are left out, the
// longest first, and named.
func bugReportLink(title string, answers map[string]string) (string, []string) {
	left := []string{}
	for {
		query := url.Values{}
		query.Set("template", bugReportForm.Form)
		if title != "" {
			query.Set("title", title)
		}
		longest := ""
		for _, field := range bugReportForm.Fields {
			answer := strings.TrimSpace(answers[field.ID])
			if answer == "" || slices.Contains(left, field.Label) {
				continue
			}
			query.Set(field.ID, answer)
			if longest == "" || len(answer) > len(strings.TrimSpace(answers[longest])) {
				longest = field.ID
			}
		}
		link := bugReportForm.NewIssue + "?" + query.Encode()
		if len(link) <= bugReportLinkLimit || longest == "" {
			return link, left
		}
		for _, field := range bugReportForm.Fields {
			if field.ID == longest {
				left = append(left, field.Label)
			}
		}
	}
}

type bugReportIssue struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	State    string `json:"state"`
	Comments int    `json:"comments"`
}

var bugReportSearchWord = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}'._-]*`)

// bugReportSearchTerms are the title's words, as the issues are searched by.
func bugReportSearchTerms(title string) string {
	words := bugReportSearchWord.FindAllString(title, 12)
	return strings.Join(words, " ")
}

// bugReportSearchPage is the issues list searched for the title's words,
// for the person or an agent to look through.
func bugReportSearchPage(title string) string {
	return bugReportForm.Issues + "?" + url.Values{"q": {strings.TrimSpace("is:issue " + bugReportSearchTerms(title))}}.Encode()
}

// bugReportSimilar asks GitHub's issue search for issues with the title's
// words, open ones first. It is public and asks no sign-in; what it can't
// answer, the note says, and the search page is still there.
func bugReportSimilar(ctx context.Context, title string) ([]bugReportIssue, string) {
	terms := bugReportSearchTerms(title)
	if terms == "" {
		return []bugReportIssue{}, "No title to search the issues by."
	}
	ctx, cancel := context.WithTimeout(ctx, bugReportSearchLimit)
	defer cancel()
	query := url.Values{"q": {terms + " repo:" + bugReportForm.Repository + " is:issue"}, "per_page": {"20"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, bugReportForm.Search+"?"+query.Encode(), nil)
	if err != nil {
		return []bugReportIssue{}, "GitHub's issue search couldn't be asked."
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Leafen bug report (+https://github.com/leafen-org/leafen)")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return []bugReportIssue{}, "GitHub's issue search didn't answer; look through similarSearch."
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnprocessableEntity:
		// GitHub can't search a repository it doesn't show: until the
		// repository is public, and its links open then too.
		return []bugReportIssue{}, "GitHub couldn't search " + bugReportForm.Repository + "'s issues (" + response.Status +
			"); look through similarSearch."
	case response.StatusCode != http.StatusOK:
		return []bugReportIssue{}, "GitHub's issue search answered " + response.Status + "; look through similarSearch."
	}
	found := struct {
		Items []struct {
			Number   int    `json:"number"`
			Title    string `json:"title"`
			HTMLURL  string `json:"html_url"`
			State    string `json:"state"`
			Comments int    `json:"comments"`
		} `json:"items"`
	}{}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&found); err != nil {
		return []bugReportIssue{}, "GitHub's issue search answered something unreadable; look through similarSearch."
	}
	open, closed := []bugReportIssue{}, []bugReportIssue{}
	for _, item := range found.Items {
		issue := bugReportIssue{Number: item.Number, Title: item.Title, URL: item.HTMLURL, State: item.State, Comments: item.Comments}
		if item.State == "open" {
			open = append(open, issue)
		} else {
			closed = append(closed, issue)
		}
	}
	similar := append(open, closed...)
	if len(similar) > bugReportSimilarMax {
		similar = similar[:bugReportSimilarMax]
	}
	return similar, ""
}
