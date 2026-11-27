package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// The version tools write a page's title as it was spelled then, as
// Notion's do (seen 2026-11-22 through Notion's MCP, worker w2328, three
// titles, and worker w2322 once): view-version's properties and
// get-version-changes' before and after as Markdown, as fetch writes a title
// (mcpPropertyMarkdown), but a page's mention by its address alone —
// `<mention url="…" />`, as a version's block text names one
// (mcpVersionBlockText) — a person's with their name, a date's and an
// equation's as fetch writes them; and get-version-changes' title as the
// title shows (mcpSpansAsShown), but a page's mention "@찾을 수 없음", its
// name not read ("w2328 날짜 @2026년 12월 25일 와 @찾을 수 없음 끝",
// "w2328 사람 @〈이름〉 수식 $x^2$ 날짜 …", a date in the words the shown
// form gives every title). A version's title is spelled
// by the spans its history kept (pageHistoryPageState.TitleSpans), a
// database page's by its title cell's then (mcpVersionRowReader.values);
// one kept without them is its words.

// mcpVersionTitleSpans are the spans a version's title was spelled by; none
// for one its history kept as words alone.
func mcpVersionTitleSpans(app core.App, pageID string, version pageHistorySnapshotResponse) []graphTextSpan {
	if version.PageID == "" {
		return nil
	}
	state, err := readPageHistorySnapshotPageState(app, pageID, version.Revision)
	if err != nil {
		return nil
	}
	spans := decodeStoredTextSpans(state.TitleSpans)
	if len(spans) == 0 || spelledName(spans) != version.Title {
		return nil
	}
	return spans
}

// mcpVersionTitleMarkdown is a version's title as the version tools write
// it in Markdown.
func mcpVersionTitleMarkdown(app core.App, authority mcpAuthority, workspaceID, title string, spans []graphTextSpan) string {
	if title == "" && len(spans) == 0 {
		return ""
	}
	mentions := mcpPropertyMentions(app, authority, workspaceID)
	mentions.PageTitle = nil
	return mcpPropertyMarkdown(title, spans, mentions)
}

// mcpVersionTitleShown is a version's title as get-version-changes names a
// change to it.
func mcpVersionTitleShown(app core.App, authority mcpAuthority, workspaceID, title string, spans []graphTextSpan) string {
	if !mcpSpansMarkTitle(spans) {
		return mcpPageShownTitle(app, authority.PrincipalID, title)
	}
	language, err := serverTextLanguage(app, authority.PrincipalID, nil)
	if err != nil {
		language = accountLanguageKo
	}
	shown := make([]graphTextSpan, 0, len(spans))
	for _, span := range spans {
		if span.Mention == "page" || span.Mention == "database" {
			span = graphTextSpan{Text: "@" + missingMentionLabel(language)}
		}
		shown = append(shown, span)
	}
	return mcpPageShownTitle(app, authority.PrincipalID, mcpSpansAsShown(app, authority, workspaceID, shown, spelledName(shown)))
}

// mcpVersionTitleChanges writes the changes to a page's title as the
// version tools do, by the two versions they compare.
func mcpVersionTitleChanges(
	app core.App, authority mcpAuthority, page *core.Record, before, after pageHistorySnapshotResponse, changes []notionMCPVersionChange,
) {
	workspaceID := page.GetString("workspace")
	then, now := mcpVersionTitleSpans(app, page.Id, before), mcpVersionTitleSpans(app, page.Id, after)
	for index := range changes {
		change := &changes[index]
		if change.Type != "properties" {
			continue
		}
		change.Title = mcpVersionTitleShown(app, authority, workspaceID, after.Title, now)
		if change.Before != nil {
			change.Before = map[string]any{"title": mcpVersionTitleMarkdown(app, authority, workspaceID, before.Title, then)}
		}
		if change.After != nil {
			change.After = map[string]any{"title": mcpVersionTitleMarkdown(app, authority, workspaceID, after.Title, now)}
		}
	}
}
