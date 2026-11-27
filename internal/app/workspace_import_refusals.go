package app

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What a refused import request says to fix (request_refusal.go): the field
// as the request names it and why, after "Check the import details.".

// workspaceImportRefused answers an import request refused for reason.
func workspaceImportRefused(event *core.RequestEvent, reason string) error {
	return apiError(event, http.StatusBadRequest, errorCodeWorkspaceImportInvalid,
		refusalMessage("Check the import details.", reason))
}

// workspaceImportBodyRefused answers a body that doesn't read as the request.
func workspaceImportBodyRefused(event *core.RequestEvent, request string, err error) error {
	return workspaceImportRefused(event, bodyRefusalReason(request, err))
}

// durableRequestIDProblem is why requestId can't name a request, or "".
func durableRequestIDProblem(requestID string) string {
	switch count := len([]rune(requestID)); {
	case requestID == "":
		return "requestId is empty: give a new id of yours, so the same request sent again isn't done twice"
	case count > maxDurableJobRequestIDRunes:
		return fmt.Sprintf("requestId is %d characters, over %d", count, maxDurableJobRequestIDRunes)
	}
	return ""
}

// workspaceImportTextProblem is why an imported text is too large, or "".
func workspaceImportTextProblem(field, text string) string {
	if size := len(text); size > workspaceImportRawTextBytes {
		return fmt.Sprintf("%s is %d bytes, over %d", field, size, workspaceImportRawTextBytes)
	}
	return ""
}

// workspaceImportInputProblem is why an import request can't run, or "".
func workspaceImportInputProblem(input workspaceImportInput) string {
	switch count := len([]rune(input.Title)); {
	case input.Title == "":
		return "title is empty: name the page or database the import makes"
	case count > maxGraphTitleRunes:
		return fmt.Sprintf("title is %d characters, over %d", count, maxGraphTitleRunes)
	case len(input.ParentPageID) > maxGraphIdentifierLength:
		return fmt.Sprintf("parentPageId is %d bytes, over %d", len(input.ParentPageID), maxGraphIdentifierLength)
	case len(input.TeamspaceID) > maxWorkspacePermissionIdentifierLength:
		return fmt.Sprintf("teamspaceId is %d bytes, over %d", len(input.TeamspaceID), maxWorkspacePermissionIdentifierLength)
	case input.ParentPageID != "" && input.TeamspaceID != "":
		return "parentPageId and teamspaceId are both given: the import goes under a page or at a teamspace's top, one of them"
	case len(input.DataSourceID) > maxGraphIdentifierLength:
		return fmt.Sprintf("dataSourceId is %d bytes, over %d", len(input.DataSourceID), maxGraphIdentifierLength)
	case input.DataSourceID != "" && (input.ParentPageID != "" || input.TeamspaceID != ""):
		return "dataSourceId is given with parentPageId or teamspaceId: the import goes into a database as its rows, under a page or at a teamspace's top, one of them"
	case input.DataSourceID != "" && input.Format != workspaceImportFormatMarkdown:
		return "dataSourceId goes with format \"markdown\": a file goes into a database as its row; a CSV's rows join one through its own import"
	case input.Locale != "" && input.Locale != workspaceImportLocaleEnglish && input.Locale != workspaceImportLocaleKorean:
		return fmt.Sprintf("locale %q isn't %q or %q", input.Locale, workspaceImportLocaleEnglish, workspaceImportLocaleKorean)
	}
	switch input.Format {
	case workspaceImportFormatMarkdown:
		switch {
		case input.CSV != "":
			return "csv goes with format \"csv\"; a Markdown import sends markdown"
		case len(input.CSVColumnTypes) != 0 || input.CSVKeepFirstRow:
			return "csvColumnTypes and csvKeepFirstRow go with format \"csv\""
		}
		return workspaceImportTextProblem("markdown", input.Markdown)
	case workspaceImportFormatCSV:
		if input.Markdown != "" {
			return "markdown goes with format \"markdown\"; a CSV import sends csv"
		}
		if problem := workspaceImportTextProblem("csv", input.CSV); problem != "" {
			return problem
		}
		if !validWorkspaceImportCSVColumnTypes(input.CSVColumnTypes) {
			return fmt.Sprintf("csvColumnTypes is left out, or a type for each column (up to %d) with one title: %s, a date as \"date:<format>\"",
				maxWorkspaceCSVMergeColumns, strings.Join(sortedWorkspaceImportNames(workspaceImportCSVColumnTypes), ", "))
		}
		return ""
	}
	return fmt.Sprintf("format %q isn't %q or %q", input.Format, workspaceImportFormatMarkdown, workspaceImportFormatCSV)
}

// sortedWorkspaceImportNames are a set's names in order, the empty one left
// out.
func sortedWorkspaceImportNames(names map[string]bool) []string {
	return slices.DeleteFunc(strings.Split(graphNamesList(names), ", "), func(name string) bool { return name == "" })
}

// workspaceImportListProblem is why a list a request sends is empty or too
// long for one request, or "".
func workspaceImportListProblem(field string, count, most int) string {
	switch {
	case count == 0:
		return field + " is empty"
	case count > most:
		return fmt.Sprintf("%s holds %d, over %d a request: send the rest in another", field, count, most)
	}
	return ""
}

// notionImportInputProblem is why a Notion import can't start, or "".
func notionImportInputProblem(input notionImportJobInput, rootsValid bool) string {
	switch {
	case input.ConnectionID == "":
		return "connectionId is empty: the Notion connection the import reads through"
	case !input.CopyFiles:
		return "copyFiles is true: an import copies the pages' files"
	case !rootsValid:
		return fmt.Sprintf("rootIds are one or more different Notion page or database ids (up to %d bytes each), with rootKinds left out or \"page\" or \"database\" for each",
			maxWorkspacePermissionIdentifierLength)
	}
	return durableRequestIDProblem(input.RequestID)
}
