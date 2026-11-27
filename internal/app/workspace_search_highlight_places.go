package app

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// workspaceSearchHighlightWords are the words of the keywords a highlight
// looks for: every word, a phrase's too, as the keywords split them
// (workspace_search_keywords.go).
func workspaceSearchHighlightWords(plainQuery string) []string {
	words := []string{}
	for _, field := range strings.Fields(plainQuery) {
		for _, word := range workspaceSearchWords(field) {
			if !slices.Contains(words, word) {
				words = append(words, word)
			}
		}
	}
	return words
}

// workspaceSearchKeywordsFound are where a line holds a keyword as a
// highlight takes it, in the line's characters and in order, and which of
// the words it is (workspace_search_weight.go): a whole word, a word's stem,
// or a part of a word of letters and digits ("Morvenal" of "Morvenal4291");
// a word of a script written without spaces anywhere in a word, and a word
// of an English keyword's stem, the whole word (seen 2026-11-16 in two tabs,
// on pages 5 to 18 minutes old: for "grindle" "grindles line three", for
// "grindles" "grindle line one"). A word only starting with a keyword
// ("morvenalx") or holding it inside ("premorvenal") is none, as Notion's
// highlights leave such lines out (seen 2026-11-11: for "morvenal" it showed
// "w398 d morvenal-tail", "w398 e Morvenal4291" and "w398 g morvenal end",
// not "w398 b morvenalx" nor "w398 c premorvenal"). Notion's stemmer reads
// by a dictionary, so for made-up words it leaves out some lines the app's
// Porter2 stem takes ("w398 f morvenals" for "morvenal"; 2026-11-16, two
// tabs: "yelbrins beta" for "yelbrin", the other word's lines for "yelbrins",
// "pobbler" and "pobblers").
func workspaceSearchKeywordsFound(shown []rune, words []string) []workspaceSearchKeywordAt {
	folded, of := workspaceSearchFoldedRunes(shown)
	found := []workspaceSearchKeywordAt{}
	stems := make([]string, len(words))
	for index, keyword := range words {
		if searchEnglishWord(keyword) {
			stems[index] = englishStem(keyword)
		}
	}
	workspaceSearchEachWord(folded, func(start, end int) {
		word := string(folded[start:end])
		for index, keyword := range words {
			places := workspaceSearchWholeKeyword(word, keyword)
			for _, at := range places {
				from, to := start+at, start+at+utf8.RuneCountInString(keyword)
				place := [2]int{of[from], len(shown)}
				if to < len(folded) {
					place[1] = of[to]
				}
				found = append(found, workspaceSearchKeywordAt{place: place, word: index})
			}
			if len(places) == 0 && stems[index] != "" && searchEnglishStemEqual(word, keyword, stems[index]) {
				place := [2]int{of[start], len(shown)}
				if end < len(folded) {
					place[1] = of[end]
				}
				found = append(found, workspaceSearchKeywordAt{place: place, word: index, loose: true})
			}
		}
	})
	slices.SortStableFunc(found, func(a, b workspaceSearchKeywordAt) int { return a.place[0] - b.place[0] })
	return found
}

func workspaceSearchWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// workspaceSearchWholeKeyword is where in a folded word, in characters, a
// keyword stands whole.
func workspaceSearchWholeKeyword(word, keyword string) []int {
	places := []int{}
	if searchPairedWord(keyword) {
		for from := 0; ; {
			index := strings.Index(word[from:], keyword)
			if index < 0 {
				return places
			}
			from += index
			places = append(places, utf8.RuneCountInString(word[:from]))
			from += len(keyword)
		}
	}
	if word == keyword {
		return []int{0}
	}
	// A stem is the word without the particles joined to its front.
	for _, stem := range searchWordStems(word) {
		if stem == keyword {
			return []int{utf8.RuneCountInString(word) - utf8.RuneCountInString(stem)}
		}
	}
	at := 0
	for _, part := range searchWordParts(word) {
		if part == keyword {
			places = append(places, at)
		}
		at += utf8.RuneCountInString(part)
	}
	return places
}
