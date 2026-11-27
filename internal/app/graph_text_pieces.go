package app

import (
	"reflect"
	"unicode/utf16"

	"github.com/reearth/ygo/crdt"
)

// graphTextPieceKey is the formatting that keeps two neighbouring pieces of
// a block's collaborative text apart when they read alike: two mentions of
// the same page with the same marks, or two equal equations, each a piece of
// its own, as Notion keeps them (seen 2026-11-11: two mentions of one page
// side by side read back as two), and the pieces of a text page Markdown
// rewrites with its words and marks as they are, as Notion leaves them
// (notionMarkdownKeptPieces). The collaborative text joins neighbouring
// runs whose formatting is the same, so every other piece of such a row
// carries it; readers leave it out, and a writer gives it again from where
// the pieces stand, so the mark of each is the same from every writer.
const graphTextPieceKey = "piece"

// graphTextAtomic reports whether a run of a text is a piece that never
// joins its neighbour: a mention or an equation.
func graphTextAtomic(attributes crdt.Attributes) bool {
	mention, _ := attributes["mention"].(string)
	equation, _ := attributes["equation"].(string)
	return mention != "" || equation != ""
}

// graphTextSameFormatting reports whether two runs read alike: the same
// formatting but for the piece mark, an absent one the same as one set to
// nothing.
func graphTextSameFormatting(left, right crdt.Attributes) bool {
	set := func(attributes crdt.Attributes) map[string]any {
		values := make(map[string]any, len(attributes))
		for key, value := range attributes {
			if key == graphTextPieceKey || value == nil || value == false || value == "" {
				continue
			}
			values[key] = value
		}
		return values
	}
	return reflect.DeepEqual(set(left), set(right))
}

// graphTextPieced gives each of a text's runs, in turn, the piece mark that
// keeps it apart from the run before it where both are mentions or
// equations that read alike: the second, fourth … of such a row carry it,
// and every other run carries none. It changes the runs' formatting in place.
func graphTextPieced(runs []crdt.Attributes) {
	var previous crdt.Attributes
	apart := false
	for _, attributes := range runs {
		if attributes == nil {
			continue
		}
		if previous != nil && graphTextAtomic(attributes) && graphTextSameFormatting(previous, attributes) {
			apart = !apart
		} else {
			apart = false
		}
		if apart {
			attributes[graphTextPieceKey] = true
		} else {
			attributes[graphTextPieceKey] = nil
		}
		previous = attributes
	}
}

// graphTextDeltaPieced is a text's inserts with the piece marks that keep
// their pieces apart (graphTextPieced); runs with no formatting of their own
// keep none.
func graphTextDeltaPieced(deltas []crdt.Delta) []crdt.Delta {
	runs := make([]crdt.Attributes, len(deltas))
	for index, delta := range deltas {
		if delta.Op != crdt.DeltaOpInsert {
			continue
		}
		runs[index] = crdt.Attributes{}
		for key, value := range delta.Attributes {
			runs[index][key] = value
		}
	}
	graphTextPieced(runs)
	result := make([]crdt.Delta, len(deltas))
	for index, delta := range deltas {
		if runs[index] != nil && (len(delta.Attributes) > 0 || runs[index][graphTextPieceKey] != nil) {
			delta.Attributes = runs[index]
		}
		result[index] = delta
	}
	return result
}

// graphTextSpansAttributes is each span's formatting in a block's
// collaborative text, every key named, with the piece marks that keep its
// pieces apart; an empty span's is nil, but for an empty piece's
// (graph_text_empty_pieces.go), which stands between its neighbours.
func graphTextSpansAttributes(spans []graphTextSpan) []crdt.Attributes {
	runs := make([]crdt.Attributes, len(spans))
	for index, span := range spans {
		if span.Text != "" || graphTextEmptyPieceSpan(span) {
			runs[index] = graphTextAttributes(span)
		}
	}
	graphTextPieced(runs)
	return runs
}

// insertGraphTextSpans writes spans, in turn, into a block's empty
// collaborative text, each with its own formatting and its pieces apart, an
// empty piece as its embed.
func insertGraphTextSpans(transaction *crdt.Transaction, text *crdt.YText, spans []graphTextSpan) {
	position := 0
	for index, attributes := range graphTextSpansAttributes(spans) {
		switch {
		case attributes == nil:
		case graphTextEmptyPieceSpan(spans[index]):
			attributes[graphTextPieceKey] = nil
			text.InsertEmbed(transaction, position, graphTextEmptyPieceEmbed(spans[index].EmptyPiece), attributes)
			position++
		default:
			written := composedText(spans[index].Text) // stored composed (composed_text.go)
			text.Insert(transaction, position, written, attributes)
			position += len(utf16.Encode([]rune(written)))
		}
	}
}
