package app

import (
	"math"
	"slices"
	"strings"
	"unicode"
)

// leafen_operations' query is a shortcut over the groups: English words,
// each found where an operation's or a tool's name, keywords, summary or
// path holds it, the word's endings trimmed on both sides (shares, sharing
// and shared are share), and weighed by how few hold it — a word most of
// them hold says little. Nothing reads what the words mean: an agent picks
// from what they find, or from the groups (native_operation_groups.go).

// nativeOperationFindText is what one operation or tool is found by, each
// part's words with their endings trimmed, and its keywords as they are
// written, each a phrase of words (native_operation_find_phrases.go).
type nativeOperationFindText struct {
	name, keywords, summary, path []string
	phrases                       [][]string
}

// How much a word weighs where it is held: in a name or the keywords most,
// in a summary less, in a path least; held as the start of a longer word,
// half that.
const (
	nativeOperationFindNameWeight    = 3
	nativeOperationFindSummaryWeight = 2
	nativeOperationFindPathWeight    = 1
	nativeOperationFindPrefixShare   = 0.5
	// nativeOperationFindFloor is the share of the best score a find needs.
	nativeOperationFindFloor = 0.2
	// nativeOperationFindTools is how many tools a search gives at most.
	nativeOperationFindTools = 5
)

// nativeOperationFindStopWords join a request's words and name nothing, the
// particles of a phrasal verb among them: the few names that hold on or up
// (decideMCPDeviceConnectionOnScreen) would weigh them as rare words, and
// "turn on backups" find the screen's approvals, not the backups.
var nativeOperationFindStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "to": true, "of": true, "for": true, "my": true, "me": true, "i": true, "it": true,
	"its": true, "this": true, "that": true, "these": true, "those": true, "and": true, "or": true, "please": true,
	"want": true, "need": true, "would": true, "like": true, "can": true, "could": true, "should": true, "how": true,
	"do": true, "does": true, "is": true, "are": true, "be": true, "with": true, "from": true, "at": true, "by": true,
	"as": true, "into": true, "so": true, "you": true, "your": true, "we": true, "our": true, "let": true, "some": true,
	"any": true, "there": true, "here": true, "via": true, "leafen": true, "on": true, "off": true, "in": true, "out": true,
	"up": true, "down": true, "over": true, "now": true, "then": true, "also": true, "just": true, "someone": true, "something": true,
}

// nativeOperationFindStem trims an English word's endings, so the forms of
// a word meet in one: plural s, -ing and -ed, a final e, a y after a
// consonant as i, a doubled last consonant (stopped, stop), and -ation,
// -ion after t or s and -al on long words (invitation and invite, deletion
// and delete, removal and remove).
func nativeOperationFindStem(word string) string {
	w := strings.TrimSuffix(strings.ToLower(word), "'s")
	vowel := func(text string) bool { return strings.ContainsAny(text, "aeiouy") }
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		w = w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "sses"):
		w = w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is"):
		w = w[:len(w)-1]
	}
	switch {
	case len(w) >= 6 && strings.HasSuffix(w, "ing") && vowel(w[:len(w)-3]):
		w = w[:len(w)-3]
	case len(w) >= 5 && strings.HasSuffix(w, "ed") && vowel(w[:len(w)-2]):
		w = w[:len(w)-2]
	case len(w) >= 10 && strings.HasSuffix(w, "ation"):
		w = w[:len(w)-5]
	case len(w) >= 6 && (strings.HasSuffix(w, "tion") || strings.HasSuffix(w, "sion")):
		w = w[:len(w)-3]
	case len(w) >= 7 && strings.HasSuffix(w, "al"):
		w = w[:len(w)-2]
	}
	if len(w) > 3 && strings.HasSuffix(w, "e") {
		w = w[:len(w)-1]
	}
	if last := len(w) - 1; len(w) > 3 && w[last] == 'y' && !strings.ContainsRune("aeiou", rune(w[last-1])) {
		w = w[:last] + "i"
	}
	if last := len(w) - 1; len(w) > 3 && w[last] == w[last-1] && !strings.ContainsRune("aeiouylsz", rune(w[last])) {
		w = w[:last]
	}
	return w
}

// nativeOperationFindWords are a text's words, without those that only join
// a request, their endings trimmed; a name's capitals start words
// (createPage is create and page).
func nativeOperationFindWords(text string) []string {
	words := []string{}
	for _, field := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		for _, word := range nativeOperationHandlerWords(field) {
			if !nativeOperationFindStopWords[word] {
				words = append(words, nativeOperationFindStem(word))
			}
		}
	}
	return words
}

// nativeOperationFindEnglish is whether a query is written in English's
// letters: one that holds a letter of another script isn't searched.
func nativeOperationFindEnglish(query string) bool {
	for _, r := range query {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return false
		}
	}
	return true
}

// nativeOperationFindMethodWords are what an operation's method says it
// does, as its id says it: a path holds what it acts on, and the method what
// it does to it (2026-11-26: "create a new workspace" found nothing of POST
// /workspaces, named postWorkspaces, and "remove my authenticator app"
// nothing of DELETE /account/mfa/totp). They weigh as the path's words.
var nativeOperationFindMethodWords = map[string]string{
	"GET":    "get read list show view see",
	"POST":   "create add make new run send start",
	"PUT":    "set change update save turn",
	"PATCH":  "change update edit rename",
	"DELETE": "delete remove cancel revoke clear drop",
}

// nativeOperationFindTextOf is what an operation is found by.
func nativeOperationFindTextOf(operation nativeOperation) nativeOperationFindText {
	entry := nativeOperationGroupEntryOf(operation.ID)
	method, path, _ := strings.Cut(operation.ID, " ")
	operation.Method = method
	static := []string{}
	for _, segment := range strings.Split(path, "/") {
		if segment != "" && !strings.HasPrefix(segment, "{") {
			static = append(static, segment)
		}
	}
	// A path's last word names what it acts on, or what it does to it, as a
	// name does: a handler named list, create or retry says it no other way
	// (2026-11-26: "list the workspace webhooks" put the integrations'
	// webhooks, whose name holds them, before GET .../webhooks, named list).
	last := ""
	if len(static) > 0 {
		last = static[len(static)-1]
	}
	return nativeOperationFindText{
		name:     nativeOperationFindWords(operation.Name + " " + last),
		keywords: nativeOperationFindWords(strings.Join(entry.Keywords, " ")),
		summary:  nativeOperationFindWords(nativeOperationSummary(operation)),
		path:     nativeOperationFindWords(strings.Join(static, " ") + " " + nativeOperationFindMethodWords[operation.Method]),
		phrases:  nativeOperationFindPhrases(entry.Keywords),
	}
}

// nativeOperationFinder weighs texts by a query's words.
type nativeOperationFinder struct {
	words []string
	// held is how many of the texts hold each word, of total.
	held  map[string]int
	total int
	// tokens are the query's words as written, joining words too, and
	// joinersOnly says it holds no other (native_operation_find_phrases.go).
	tokens      []string
	joinersOnly bool
}

// newNativeOperationFinder reads a query's words and how many of the texts
// hold each.
func newNativeOperationFinder(query string, texts []nativeOperationFindText) nativeOperationFinder {
	finder := nativeOperationFinder{held: map[string]int{}, total: len(texts), tokens: nativeOperationFindTokens(query)}
	for _, word := range nativeOperationFindWords(query) {
		if !slices.Contains(finder.words, word) {
			finder.words = append(finder.words, word)
		}
	}
	finder.joinersOnly = len(finder.words) == 0
	for _, text := range texts {
		for _, word := range finder.words {
			if text.weight(word) > 0 {
				finder.held[word]++
			}
		}
	}
	return finder
}

// unheld are the query's words no text holds, as the query wrote them.
func (finder nativeOperationFinder) unheld(query string) []string {
	unheld := []string{}
	for _, field := range strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		for _, word := range nativeOperationHandlerWords(field) {
			if !nativeOperationFindStopWords[word] && finder.held[nativeOperationFindStem(word)] == 0 && !slices.Contains(unheld, word) {
				unheld = append(unheld, word)
			}
		}
	}
	return unheld
}

// weight is how much a text's best place for the word weighs.
func (text nativeOperationFindText) weight(word string) float64 {
	best := 0.0
	for _, part := range []struct {
		words  []string
		weight float64
	}{{text.name, nativeOperationFindNameWeight}, {text.keywords, nativeOperationFindNameWeight},
		{text.summary, nativeOperationFindSummaryWeight}, {text.path, nativeOperationFindPathWeight}} {
		for _, held := range part.words {
			switch {
			case held == word:
				best = max(best, part.weight)
			case len(word) >= 4 && strings.HasPrefix(held, word):
				best = max(best, part.weight*nativeOperationFindPrefixShare)
			}
		}
	}
	return best
}

// score is what a text weighs for the query: each word it holds by its
// place and by how few hold it, times the share it holds of the query's
// words that any text holds, each by how few hold it — a word no operation
// or tool holds, or one most hold, takes little from a text that holds the
// rest.
func (finder nativeOperationFinder) score(text nativeOperationFindText) float64 {
	// A query of joining words alone is found only where a keyword is just
	// those words (like, me).
	if finder.joinersOnly {
		if text.phraseBoost(finder) > 1 {
			return nativeOperationFindNameWeight
		}
		return 0
	}
	score, held, asked := 0.0, 0.0, 0.0
	for _, word := range finder.words {
		if finder.held[word] == 0 {
			continue
		}
		rare := math.Log(1 + float64(finder.total)/float64(1+finder.held[word]))
		asked += rare
		if weight := text.weight(word); weight > 0 {
			held += rare
			score += weight * rare
		}
	}
	if held == 0 {
		return 0
	}
	// A keyword the query holds as it is written, its joining words and
	// endings too, says it more than its trimmed words do.
	return score * held / asked * text.phraseBoost(finder)
}

// nativeOperationFindHit is one find: its place among the texts and its score.
type nativeOperationFindHit struct {
	index int
	score float64
}

// nativeOperationFindRanked are the texts the query finds, best first: those
// that weigh at least nativeOperationFindFloor of the best.
func nativeOperationFindRanked(finder nativeOperationFinder, texts []nativeOperationFindText) []nativeOperationFindHit {
	scores, best := make([]float64, len(texts)), 0.0
	for index, text := range texts {
		scores[index] = finder.score(text)
		best = max(best, scores[index])
	}
	found := []nativeOperationFindHit{}
	for index, score := range scores {
		if score > 0 && score >= best*nativeOperationFindFloor {
			found = append(found, nativeOperationFindHit{index: index, score: score})
		}
	}
	slices.SortStableFunc(found, func(a, b nativeOperationFindHit) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return 0
	})
	return found
}
