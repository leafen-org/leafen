package app

import (
	"encoding/json"

	"github.com/reearth/ygo/crdt"
)

// pageHistoryTextRun is a piece of a block's text in the history with its
// formatting, mentions and equations, as the page's text keeps them.
type pageHistoryTextRun struct {
	Insert     string         `json:"insert"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// pageHistoryTextRunsFromDelta is a block's text, read from its page's
// document, as history runs.
func pageHistoryTextRunsFromDelta(deltas []crdt.Delta) []pageHistoryTextRun {
	runs := []pageHistoryTextRun{}
	for _, delta := range deltas {
		if text, ok := delta.Insert.(string); delta.Op == crdt.DeltaOpInsert && ok && text != "" {
			runs = append(runs, pageHistoryTextRun{Insert: text, Attributes: pageHistoryFormatted(nil, delta.Attributes)})
		}
	}
	return runs
}

func pageHistoryTextRunsText(runs []pageHistoryTextRun) string {
	text := make([]byte, 0)
	for _, run := range runs {
		text = append(text, run.Insert...)
	}
	return string(text)
}

func encodePageHistoryTextRuns(runs []pageHistoryTextRun) (string, error) {
	encoded, err := json.Marshal(runs)
	return string(encoded), err
}

// decodePageHistoryTextRuns reads stored runs; a span kept before the history
// kept formatting has none, and its text is then known only as plain text.
func decodePageHistoryTextRuns(plain, rich string) ([]pageHistoryTextRun, bool) {
	if rich == "" {
		return []pageHistoryTextRun{{Insert: plain}}, plain == ""
	}
	runs := []pageHistoryTextRun{}
	if json.Unmarshal([]byte(rich), &runs) != nil || pageHistoryTextRunsText(runs) != plain {
		return []pageHistoryTextRun{{Insert: plain}}, false
	}
	return runs, true
}

// pageHistoryFormatted is attributes with a change's formatting applied: a
// value sets its attribute and a null clears it.
func pageHistoryFormatted(base map[string]any, change map[string]any) map[string]any {
	if len(change) == 0 {
		return base
	}
	next := make(map[string]any, len(base)+len(change))
	for key, value := range base {
		next[key] = value
	}
	for key, value := range change {
		if value == nil {
			delete(next, key)
		} else {
			next[key] = value
		}
	}
	if len(next) == 0 {
		return nil
	}
	return next
}

// pageHistoryDocument is a kept text document state, read to give its
// blocks' runs.
func pageHistoryDocument(state []byte) (*crdt.Doc, error) {
	document := crdt.New()
	if len(state) == 0 {
		return document, nil
	}
	return document, crdt.ApplyUpdateV1(document, state, nil)
}

func pageHistoryBlockTextRuns(document *crdt.Doc, blockID string) []pageHistoryTextRun {
	return pageHistoryTextRunsFromDelta(document.GetText(ygoBlockTextRootPrefix + blockID).ToDelta())
}

// pageHistoryContentWithRich gives a version's block its text's spans as
// content.rich, as the page's blocks carry them; a block whose history kept
// no formatting, or with no text, keeps its content.
func pageHistoryContentWithRich(content, rich, plain string) string {
	runs, known := decodePageHistoryTextRuns(plain, rich)
	if !known || plain == "" {
		return content
	}
	object := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(content), &object) != nil || object == nil {
		return content
	}
	if _, has := object["rich"]; has {
		return content
	}
	deltas := make([]crdt.Delta, len(runs))
	for index, run := range runs {
		deltas[index] = crdt.Delta{Op: crdt.DeltaOpInsert, Insert: run.Insert, Attributes: run.Attributes}
	}
	spans, err := json.Marshal(graphTextSpansFromDelta(deltas))
	if err != nil {
		return content
	}
	object["rich"] = spans
	encoded, err := json.Marshal(object)
	if err != nil {
		return content
	}
	return string(encoded)
}
