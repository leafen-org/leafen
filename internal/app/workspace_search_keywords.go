package app

import (
	"slices"
	"strings"
	"unicode"
)

// A search's keywords are read in one place, which every search asks — the
// app's, MCP's and the official API's (notion_search_api.go) — and judged by
// one rule, in the index (workspace_search_order.go) and in a text alike. A
// phrase in double quotes is exactly those words, whole, in that order: not
// a part of a word of letters and digits, nor a piece of a word of a script
// written without spaces. Every other word is the start of a word, of its
// stem, or of a part of a word of letters and digits ("4291" finds
// "Tarvo4291"), or, an English word, a word of its stem ("runs" finds
// "running": search_english_stem.go), and a word of a script written without spaces is found
// anywhere in a run of it (workspace_search_language.go); a number's word is
// a word or such a part whole, the last word typed in a title its start too
// (searchNumberWord). A page matches all
// of them, or, listed after those, every phrase and some of the other words,
// as Notion's search does in the app and its API (seen 2026-11-11:
// `"zorvek kravnid" vandrel` finds only the page holding both; `zorvek
// vandrel` finds a page holding either; `"zorvek kravnid" "kravnid of"` finds
// a page holding both phrases only) — a word before the last typed held
// whole, there alone (someWhole).

type workspaceSearchKeywords struct {
	// Phrases are the quoted phrases' words, each phrase's in order.
	Phrases [][]string
	// Words are the other words.
	Words []string
	// Joined are the runs of the other words written joined, with no space
	// between them ("11/23", "2026-11-23"), each run's words in order: a
	// page holding a run as written comes first (workspace_search_joined.go).
	Joined [][]string
	// TitleStart is the last of Words when it is the last word typed and a
	// number's: a title holds it as the start of a word too, as the title is
	// typed (searchNumberWord).
	TitleStart string
	// EndsInWord is whether the last of Words is the last word typed, no
	// phrase after it.
	EndsInWord bool
}

// readWorkspaceSearchKeywords reads folded keywords.
func readWorkspaceSearchKeywords(query string) workspaceSearchKeywords {
	keywords := workspaceSearchKeywords{}
	for _, part := range workspaceSearchQueryParts(query) {
		words := workspaceSearchWords(part.text)
		switch {
		case len(words) == 0:
		case part.phrase:
			keywords.Phrases = append(keywords.Phrases, words)
			keywords.TitleStart, keywords.EndsInWord = "", false
		default:
			keywords.Words = append(keywords.Words, words...)
			keywords.Joined = append(keywords.Joined, workspaceSearchJoinedRuns(part.text)...)
			keywords.TitleStart, keywords.EndsInWord = "", true
			if last := words[len(words)-1]; searchNumberWord(last) {
				keywords.TitleStart = last
			}
		}
	}
	return keywords
}

// titleStarts is whether the word at an index of Words is TitleStart.
func (keywords workspaceSearchKeywords) titleStarts(index int) bool {
	return keywords.TitleStart != "" && index == len(keywords.Words)-1
}

// someWhole is whether a page matching some keywords, not all, holds the word
// at an index of Words only whole — a word, stem or part (holdsWordWhole): a
// word before the last typed, as Notion's search lists for "vorpalt zzqk"
// the title "… vorpalt", the page under it and "… vorpalton zzqk", not
// "… vorpalton", "… vorpaltine", the page under "… vorpalton" nor a body
// holding "vorpaltix", and for "zzqk vorpalt" all of them but the body (seen
// 2026-11-24 in two tabs). A word of a script written without spaces is
// held anywhere in a run as ever ("단풍 zzqk" lists "… 단풍나무").
func (keywords workspaceSearchKeywords) someWhole(index int) bool {
	last := keywords.EndsInWord && index == len(keywords.Words)-1
	return !last && !searchPairedWord(keywords.Words[index])
}

func (keywords workspaceSearchKeywords) empty() bool {
	return len(keywords.Phrases) == 0 && len(keywords.Words) == 0
}

// wordParts are the parts of the words of letters and digits (searchWordParts),
// each a word some of the words find a page by.
func (keywords workspaceSearchKeywords) wordParts() []string {
	parts := []string{}
	for _, word := range keywords.Words {
		parts = append(parts, searchWordParts(word)...)
	}
	return parts
}

// matchesSome is whether a page matching some of the words, not all, is
// one too: with more than one word, or a word of letters and digits.
func (keywords workspaceSearchKeywords) matchesSome() bool {
	return len(keywords.Words) > 1 || len(keywords.wordParts()) > 0
}

type workspaceSearchQueryPart struct {
	text   string
	phrase bool
}

// workspaceSearchQueryParts splits keywords into quoted phrases and loose
// words; a quote left open quotes nothing.
func workspaceSearchQueryParts(query string) []workspaceSearchQueryPart {
	parts := []workspaceSearchQueryPart{}
	current := strings.Builder{}
	inPhrase := false
	for _, r := range query {
		if !workspaceSearchQuote(r) {
			current.WriteRune(r)
			continue
		}
		parts = append(parts, workspaceSearchQueryPart{text: current.String(), phrase: inPhrase})
		current.Reset()
		inPhrase = !inPhrase
	}
	parts = append(parts, workspaceSearchQueryPart{text: current.String()})
	return parts
}

func workspaceSearchQuote(r rune) bool {
	return r == '"' || r == '“' || r == '”'
}

func workspaceSearchWords(text string) []string {
	words := []string{}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	}) {
		if part = strings.TrimSpace(part); part != "" {
			words = append(words, part)
		}
	}
	return words
}

// workspaceSearchPlainText is the keywords without their quotes, which a
// title holds when it matches them.
func workspaceSearchPlainText(query string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if workspaceSearchQuote(r) {
			return ' '
		}
		return r
	}, query)), " ")
}

// workspaceSearchText is a text's words as the keywords are judged against
// them: its words in order, for a phrase, and the words a word may start —
// its words, their stems and their parts; and whether it is a title, whose
// words the last number typed may start (TitleStart).
type workspaceSearchText struct {
	words  []string
	starts []string
	title  bool
}

func readWorkspaceSearchText(text string, title bool) workspaceSearchText {
	read := workspaceSearchText{words: workspaceSearchWords(foldSearchText(text)), title: title}
	for _, word := range read.words {
		read.starts = append(read.starts, word)
		read.starts = append(read.starts, searchWordStems(word)...)
		read.starts = append(read.starts, searchWordParts(word)...)
	}
	return read
}

// holdsPhrase is whether the text has the words in that order, each whole.
func (text workspaceSearchText) holdsPhrase(phrase []string) bool {
	for start := 0; start+len(phrase) <= len(text.words); start++ {
		if slices.Equal(text.words[start:start+len(phrase)], phrase) {
			return true
		}
	}
	return false
}

// holdsWord is whether the text has a word starting with the keyword or, an
// English one, a word of its stem (search_english_stem.go); one of a script
// written without spaces anywhere in a word; a number's word or part whole,
// or, the title start in a title, its start.
func (text workspaceSearchText) holdsWord(word string, titleStart bool) bool {
	if searchNumberWord(word) && !(titleStart && text.title) {
		return text.holdsPart(word)
	}
	if searchPairedWord(word) {
		for _, held := range text.words {
			if strings.Contains(held, word) {
				return true
			}
		}
		return false
	}
	for _, held := range text.starts {
		if strings.HasPrefix(held, word) {
			return true
		}
	}
	if searchEnglishWord(word) {
		stem := englishStem(word)
		for _, held := range text.words {
			if searchEnglishStemEqual(held, word, stem) {
				return true
			}
		}
	}
	return false
}

// holdsWordWhole is whether the text has the keyword whole — a word, a stem
// or a part — or, an English one, a word of its stem.
func (text workspaceSearchText) holdsWordWhole(word string) bool {
	if text.holdsPart(word) {
		return true
	}
	if searchEnglishWord(word) {
		stem := englishStem(word)
		for _, held := range text.words {
			if searchEnglishStemEqual(held, word, stem) {
				return true
			}
		}
	}
	return false
}

// holdsPart is whether the text has a word or part that is the part whole.
func (text workspaceSearchText) holdsPart(part string) bool {
	for _, held := range text.starts {
		if held == part {
			return true
		}
	}
	return false
}

// match judges names — a title, and the titles above it, say — as the index
// judges a page by them (matchTexts).
func (keywords workspaceSearchKeywords) match(titles ...string) (all, some bool) {
	read := make([]workspaceSearchText, 0, len(titles))
	for _, title := range titles {
		read = append(read, readWorkspaceSearchText(title, true))
	}
	return keywords.matchTexts(read)
}

// matchPage judges a page's title and body as the index judges the page.
func (keywords workspaceSearchKeywords) matchPage(title, body string) (all, some bool) {
	return keywords.matchTexts([]workspaceSearchText{readWorkspaceSearchText(title, true), readWorkspaceSearchText(body, false)})
}

// matchTexts is whether texts hold every keyword, and whether they hold
// every phrase and some of the other words (the page matching some
// keywords), a word before the last typed whole (someWhole). A phrase stands
// in one of them; the words may be in any.
func (keywords workspaceSearchKeywords) matchTexts(read []workspaceSearchText) (all, some bool) {
	if keywords.empty() {
		return false, false
	}
	for _, phrase := range keywords.Phrases {
		held := false
		for _, text := range read {
			held = held || text.holdsPhrase(phrase)
		}
		if !held {
			return false, false
		}
	}
	all, some = true, len(keywords.Words) == 0
	for index, word := range keywords.Words {
		held, whole := false, keywords.someWhole(index)
		for _, text := range read {
			held = held || text.holdsWord(word, keywords.titleStarts(index))
			some = some || whole && text.holdsWordWhole(word)
		}
		all, some = all && held, some || held && !whole
	}
	for _, part := range keywords.wordParts() {
		for _, text := range read {
			some = some || text.holdsPart(part)
		}
	}
	return all, some || all
}

// holdsAny is whether a line of a body holds any of the keywords: a phrase
// whole, one of the words, or a part of one, judged as matchPage judges them.
func (keywords workspaceSearchKeywords) holdsAny(text string) bool {
	if keywords.empty() || strings.TrimSpace(text) == "" {
		return false
	}
	read := readWorkspaceSearchText(text, false)
	for _, phrase := range keywords.Phrases {
		if read.holdsPhrase(phrase) {
			return true
		}
	}
	for _, word := range keywords.Words {
		if read.holdsWord(word, false) {
			return true
		}
	}
	for _, part := range keywords.wordParts() {
		if read.holdsPart(part) {
			return true
		}
	}
	return false
}

// searchPairedWord is whether a word is of a script written without spaces
// alone, which the index finds anywhere in a run of it.
func searchPairedWord(word string) bool {
	clusters := searchClusters(word)
	for _, cluster := range clusters {
		if !searchPairedCluster(cluster) {
			return false
		}
	}
	return len(clusters) > 0
}

// workspaceSearchFoundByTitle is whether a found page was found by its title:
// its title holds every keyword, or, found among the pages matching some or
// by its path (its tier, workspaceSearchCandidate.tier), its phrases and some
// words — as Notion's search lists "W388 fd row Brindavo", found by "zelquor
// brindavo" through its database "W388 Zelquor fulldb", among the pages found
// by their titles (seen 2026-11-11).
func workspaceSearchFoundByTitle(keywords workspaceSearchKeywords, title string, tier int64) bool {
	all, some := keywords.match(title)
	return all || (workspaceSearchSomeTier(tier) || tier == workspaceSearchTierPath) && some
}
