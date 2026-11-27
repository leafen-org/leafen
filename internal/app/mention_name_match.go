package app

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
)

// The "@" and "[[" menus find row pages, the databases rows hold and the
// sources of a database of several by how well their names answer what was
// typed, by the rank the app gives the workspace's pages
// (web/src/lib/pageSearchRank.ts), so that one rule mixes them all: its
// name, a name starting so, a name holding it, then all its words apart,
// then some of them — more of them first. Notion's "@" menu and its open
// list find a row holding only some of the words typed after those holding
// them all, pages and rows alike (seen 2026-11-24 in two tabs: "vobrel
// kintaq" listed the rows and pages holding both, then those holding one).

// mentionSearchRank is how well a name answers a typed query, lower first,
// lower case either side; +Inf for a name that answers none of its words.
func mentionSearchRank(name, query string) float64 {
	title, query := strings.ToLower(name), strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0
	}
	tokens := strings.Fields(query)
	if title == query {
		return 0
	}
	if strings.HasPrefix(title, query) {
		return 1
	}
	if mentionNameHoldsPiece(title, query, true) {
		return 2
	}
	held := 0
	for index, token := range tokens {
		if mentionNameHoldsPiece(title, token, index == len(tokens)-1) {
			held++
		}
	}
	if len(tokens) > 1 && held == len(tokens) {
		return 4
	}
	// Steps below one, so each rank keeps its place.
	if held > 0 {
		return 5 + float64(len(tokens)-held)/float64(len(tokens)+1)
	}
	// A word of letters and digits is words of its own there: ab06 finds
	// AB02 (seen 2026-11-05), the last of them typed as a start too: "zzqb
	// kv7q" finds "… qxomel", "7q zzqb" only "… q" (seen 2026-11-24).
	if parts := mentionWordParts(query); len(parts) > len(tokens) {
		words := mentionWordParts(title)
		for index, part := range parts {
			for _, word := range words {
				if word == part || index == len(parts)-1 && strings.HasPrefix(word, part) {
					return 6
				}
			}
		}
	}
	return math.Inf(1)
}

// mentionNameHolds is whether a name answers what was typed in a "@" or
// "[[" menu at all (mentionSearchRank).
func mentionNameHolds(name, query string) bool {
	return !math.IsInf(mentionSearchRank(name, query), 1)
}

// mentionNameHoldsPiece is whether a name holds a piece of what was typed
// where one of its words starts — or a part of one, its letters and its
// digits each a part, as "kv7q" is kv, 7 and q — and, but for the last piece
// typed, where that word or part ends: whole. Never inside a word: "@qx"
// finds "… qx" and "… qxomel", not "… boqxel" nor "… meqx"; "@zorvel qx
// dintak" only "… qx"; "@561" no "w2561"; "@1234" no "51234", "@7123 zzzq"
// no "71234 w", "@7123" does — as Notion's "@" menu, page link picker and
// search find them (seen 2026-11-24 in test workspaces; the app's
// titleHolds in web/src/lib/pageSearchRank.ts, the search's
// searchNumberWord). A piece of a script written without spaces is held
// anywhere in a run of it, as search finds it (searchPairedScript).
func mentionNameHoldsPiece(name, piece string, last bool) bool {
	first, _ := utf8.DecodeRuneInString(piece)
	end, _ := utf8.DecodeLastRuneInString(piece)
	for at := 0; at <= len(name); {
		index := strings.Index(name[at:], piece)
		if index < 0 {
			return false
		}
		index += at
		_, size := utf8.DecodeRuneInString(name[index:])
		at = index + size
		if !searchPairedScript(first) && !mentionWordEdge(mentionRuneBefore(name, index), first) {
			continue
		}
		if !last && !searchPairedScript(end) && !mentionWordEdge(mentionRuneFrom(name, index+len(piece)), end) {
			continue
		}
		return true
	}
	return false
}

// mentionWordEdge is whether a word or a part of one ends between outside
// and inside, either way round.
func mentionWordEdge(outside, inside rune) bool {
	kind := mentionRuneKind(inside)
	return kind == 0 || mentionRuneKind(outside) != kind
}

// mentionRuneKind is a character to a word's edge: none (a space, a mark of
// punctuation, nothing — -1), a digit, a letter.
func mentionRuneKind(r rune) int {
	switch {
	case r < 0:
		return 0
	case unicode.IsNumber(r):
		return 1
	case unicode.IsLetter(r) || unicode.IsMark(r):
		return 2
	}
	return 0
}

func mentionRuneBefore(text string, at int) rune {
	if at <= 0 {
		return -1
	}
	r, _ := utf8.DecodeLastRuneInString(text[:at])
	return r
}

func mentionRuneFrom(text string, at int) rune {
	if at >= len(text) {
		return -1
	}
	r, _ := utf8.DecodeRuneInString(text[at:])
	return r
}

// mentionWordParts is a text's words of letters and digits, a run of
// letters and one of digits each a word of its own.
func mentionWordParts(text string) []string {
	parts := []string{}
	var current strings.Builder
	var previous rune
	flush := func() {
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}
	for _, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsNumber(r) {
			flush()
			previous = 0
			continue
		}
		if previous != 0 && (unicode.IsLetter(previous) && unicode.IsNumber(r) || unicode.IsNumber(previous) && unicode.IsLetter(r)) {
			flush()
		}
		current.WriteRune(r)
		previous = r
	}
	flush()
	return parts
}

// mentionSearchTier is a band of mentionSearchRank a search reads at once:
// the names holding what was typed whole (ranks 0 to 2), those holding all
// its words apart (4), and those holding some of them (5 and 6).
type mentionSearchTier int

const (
	mentionTierWhole mentionSearchTier = iota
	mentionTierEvery
	mentionTierSome
)

// mentionSearchTiers is the tiers a query is read in, the better first: a
// query of one word without parts has only the first.
func mentionSearchTiers(query string) []mentionSearchTier {
	query = strings.ToLower(strings.TrimSpace(query))
	tokens := strings.Fields(query)
	if len(tokens) > 1 {
		return []mentionSearchTier{mentionTierWhole, mentionTierEvery, mentionTierSome}
	}
	if len(mentionWordParts(query)) > len(tokens) {
		return []mentionSearchTier{mentionTierWhole, mentionTierSome}
	}
	return []mentionSearchTier{mentionTierWhole}
}

// mentionTierOf is the tier a rank falls in.
func mentionTierOf(rank float64) mentionSearchTier {
	switch {
	case rank <= 2:
		return mentionTierWhole
	case rank <= 4:
		return mentionTierEvery
	default:
		return mentionTierSome
	}
}

// mentionNameInTier is whether a name answers a query within a tier.
func mentionNameInTier(name, query string, tier mentionSearchTier) bool {
	rank := mentionSearchRank(name, query)
	return !math.IsInf(rank, 1) && mentionTierOf(rank) == tier
}

// mentionTierSQL is the condition a name column (lower case on neither
// side) meets to be read for a tier, binding its words into params: as
// wide as the tier or wider (lower() folds ASCII alone, a number's word is
// held whole only in Go), each name read then checked by mentionNameInTier.
// An empty query is every name.
func mentionTierSQL(column, query string, tier mentionSearchTier, params dbx.Params) string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return "1"
	}
	holds := func(word string) string {
		key := "mentionWord" + strconv.Itoa(len(params))
		params[key] = word
		return "instr(lower(" + column + "), {:" + key + "}) > 0"
	}
	tokens := strings.Fields(query)
	switch tier {
	case mentionTierWhole:
		return holds(query)
	case mentionTierEvery:
		every := make([]string, 0, len(tokens))
		for _, token := range tokens {
			every = append(every, holds(token))
		}
		return "(" + strings.Join(every, " AND ") + ")"
	default:
		some := []string{}
		seen := map[string]bool{}
		for _, word := range append(tokens, mentionWordParts(query)...) {
			if !seen[word] {
				seen[word] = true
				some = append(some, holds(word))
			}
		}
		return "(" + strings.Join(some, " OR ") + ")"
	}
}

// fillMentionTiers reads a query's candidates a tier at a time, the better
// first, each read asked for what is still needed up to limit.
func fillMentionTiers[T any](query string, limit int, read func(tier mentionSearchTier, need int) ([]T, error)) ([]T, error) {
	found := []T{}
	for _, tier := range mentionSearchTiers(query) {
		if len(found) >= limit {
			break
		}
		more, err := read(tier, limit-len(found))
		if err != nil {
			return nil, err
		}
		found = append(found, more...)
	}
	return found, nil
}
