package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A <page> or <database> tag's title is written as its reader's Markdown
// writes text, with its formatting and mentions, as Notion's page Markdown
// writes it (seen 2026-11-10: "v1" `<database …>**bold ***<mention-page
// …>…</mention-page>* end</database>`, "v2" the same title in tags on a line
// of its own). A tag written back with its title as it was read keeps the
// page's or database's title and its formatting, and one with a new title
// renames it to that title, its formatting and mentions too, as Notion's
// does (seen 2026-11-11: "**New** <mention-page …>…</mention-page>" a bold
// "New" and a mention), the mentions named as the writer names a block's
// (notionMarkdownPreparedTitle). A plain title is plain, its characters
// escaped as its reader's text escapes them ("v2" `a\_b \<x>`, seen
// 2026-11-11).

// titled is the reference with its title as the writer writes it.
func (w *notionMarkdownWriter) titled(reference notionMarkdownReference) notionMarkdownReference {
	reference.Written = notionMarkdownEscaped.Replace(reference.Title)
	if w.mentions.Version2 {
		reference.Written = notionMarkdownVersion2EscapedText(reference.Title)
	}
	if len(reference.Spans) > 0 {
		if title, ok := notionMarkdownText(reference.Spans, w.mentions); ok {
			reference.Written = strings.NewReplacer("\n", " ", notionMarkdownCodeBreak, " ", notionMarkdownEquationLineBreak, " ").Replace(title)
		}
	}
	return reference
}

// writtenTitle is the title its tag holds: as the writer wrote it, or its
// words escaped.
func (reference notionMarkdownReference) writtenTitle() string {
	if reference.Written != "" {
		return reference.Written
	}
	return notionMarkdownEscaped.Replace(reference.Title)
}

// notionMarkdownTagTitle is the words a tag's title reads as, as the text of
// a block of its Markdown reads ("v1"'s when version1 is set), and its
// formatting and mentions, none for plain words.
func notionMarkdownTagTitle(written string, version1 bool) (string, []graphTextSpan) {
	return notionMarkdownVersionInlineSpans(written, version1)
}

// notionMarkdownSameTitle reports whether a tag gives the sub-page or
// database it names the title it has: as it was read — but for the spaces
// at its ends where it drops them (notionMarkdownTag.Trimmed) — or its words
// plain as they are.
func notionMarkdownSameTitle(tag notionMarkdownTag, reference notionMarkdownReference) bool {
	written, read := tag.Written, reference.writtenTitle()
	if tag.Trimmed {
		written, read = strings.TrimSpace(written), strings.TrimSpace(read)
	}
	return written == read || tag.Title == reference.Title && len(tag.Spans) == 0 && len(reference.Spans) == 0
}

// notionMarkdownTitledSource is a database's source titled as a tag gives
// it: plain when its title is plain words.
func notionMarkdownTitledSource(sourceID, title string, spans []graphTextSpan) *graphDataSource {
	if spans == nil {
		spans = []graphTextSpan{}
	}
	return &graphDataSource{ID: sourceID, Title: title, TitleSpans: spans}
}

// notionMarkdownPreparedTitle is the title a tag gives as a page or a
// database keeps it: its mentions named as prepare names a block's, its
// spans trimmed when trimmed is set (a "v1" page tag's, an MCP page
// title's; notionMarkdownReferenceLine) and its words the ones they spell;
// for plain words, or for spans a title can't keep, its words alone, with
// no spans.
func notionMarkdownPreparedTitle(
	title string, spans []graphTextSpan, trimmed bool, prepare func([]graphTextSpan) ([]graphTextSpan, error),
) (string, []graphTextSpan, error) {
	if len(spans) == 0 {
		return title, []graphTextSpan{}, nil
	}
	prepared, err := prepare(slices.Clone(spans))
	if err != nil {
		return "", nil, err
	}
	if trimmed {
		prepared = trimGraphTextSpans(prepared)
	}
	words := spelledName(prepared)
	encoded, _ := json.Marshal(words)
	kept, err := normalizeGraphNameSpans(encoded, prepared)
	if err != nil || len([]rune(words)) > maxGraphTitleRunes {
		return title, []graphTextSpan{}, nil
	}
	if kept == nil {
		kept = []graphTextSpan{}
	}
	return words, kept, nil
}

// prepareNotionMarkdownSourceTitles names the mentions of the titles tags
// give databases in input — those its sources take — as prepare names the
// mentions of the blocks' text, each as a text update of its own while it
// prepares.
func prepareNotionMarkdownSourceTitles(
	input graphMutationInput, prepare func(graphMutationInput) (graphMutationInput, error),
) (graphMutationInput, error) {
	titled := []int{}
	texts := len(input.TextUpdates)
	updates := slices.Clone(input.TextUpdates)
	for index, operation := range input.Operations {
		if operation.Kind == graphOperationKindUpsertDataSource && operation.DataSource != nil &&
			len(operation.DataSource.TitleSpans) > 0 {
			titled = append(titled, index)
			updates = append(updates, graphTextUpdate{Spans: slices.Clone(operation.DataSource.TitleSpans)})
		}
	}
	if len(titled) == 0 {
		return prepare(input)
	}
	input.TextUpdates = updates
	prepared, err := prepare(input)
	if err != nil {
		return prepared, err
	}
	for at, index := range titled {
		source := *prepared.Operations[index].DataSource
		source.Title, source.TitleSpans, _ = notionMarkdownPreparedTitle(source.Title, prepared.TextUpdates[texts+at].Spans, false,
			func(spans []graphTextSpan) ([]graphTextSpan, error) { return spans, nil })
		prepared.Operations[index].DataSource = &source
	}
	prepared.TextUpdates = prepared.TextUpdates[:texts]
	return prepared, nil
}

// preparedRenames are the titles the tags give sub-pages and full-page
// databases as they keep them, their mentions named as the writer's
// mention preparation names a block's (notionMarkdownPreparedTitle).
func (request pageMarkdownReplacement) preparedRenames(app core.App, renames []notionMarkdownRename) ([]notionMarkdownRename, error) {
	prepared := slices.Clone(renames)
	if len(prepared) == 0 {
		return prepared, nil
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := request.Authorize(txApp)
		if err != nil {
			return err
		}
		prepare := func(spans []graphTextSpan) ([]graphTextSpan, error) {
			if request.PrepareInput == nil {
				return spans, nil
			}
			input, err := request.PrepareInput(txApp, current, nil, graphMutationInput{TextUpdates: []graphTextUpdate{{Spans: spans}}})
			if err != nil {
				return nil, err
			}
			return input.TextUpdates[0].Spans, nil
		}
		for index := range prepared {
			rename := &prepared[index]
			if rename.Title, rename.Spans, err = notionMarkdownPreparedTitle(
				rename.Title, rename.Spans, rename.Trimmed, prepare,
			); err != nil {
				return err
			}
		}
		return nil
	})
	return prepared, err
}
