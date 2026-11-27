package app

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Search finds a word in any language (2026-10-14 decision: search language
// matching). The index reads text as words, a word being letters, digits
// and their combining marks — a vowel sign stays with its consonant, so a
// Devanagari or Myanmar word isn't cut into consonants. Scripts written
// without spaces between words, or that join words — Hangul, Han, Hiragana,
// Katakana, Thai, Lao, Khmer, Myanmar, Tibetan — are also indexed as pairs
// of characters, and a word of them is searched as its pairs in a row, so a
// word is found inside a longer run ("계약서" in "물품공급계약서를", "合同" in
// a Chinese sentence) and a two-character word as well. Diacritics fold in
// any script ("resume" finds "résumé", "hop dong" finds "hợp đồng", "иод"
// finds "йод"; foldSearchText), and an Arabic,
// Persian, Urdu or Hebrew word is also indexed without the particles joined
// to its front ("العقد" finds "والعقد", "חוזה" finds "החוזה"). A Latin word
// joined of others ("Kaufvertrag") isn't split, as search engines don't.

// foldSearchText is text as search across the workspace (⌘K) compares it,
// as Notion's compares it, either side (seen 2026-11-16 in two tabs): in
// its compatibility form ("ｃａｆｅ" is "cafe", "ﬁle" "file"), lower case,
// the final sigma as σ and ß as ss, without its diacritic marks in any
// script — accents, the Cyrillic breve ("иод" finds "йод"), Greek tonos,
// Hebrew points, Arabic vowel marks, the Devanagari virama and nukta, Thai
// tone marks — but the kana voicing marks ("かくせい" doesn't find
// "がくせい"), and with the Latin letters that carry a stroke as their letter
// (ł l, đ d, ø o, ħ h, ŧ t, ı i) and those written as two as their two (æ ae,
// œ oe, þ th, ð d): "strasse" finds "Straße", "lodz" "Łódź", "aero" "Ærø".
// A letter can fold to more than one, or none. A database view's search
// folds otherwise (foldDatabaseViewSearchText).
func foldSearchText(value string) string {
	for index := 0; index < len(value); index++ {
		if value[index] >= utf8.RuneSelf {
			return foldSearchWideText(value)
		}
	}
	return strings.ToLower(value)
}

func foldSearchWideText(value string) string {
	decomposed := strings.ToLower(norm.NFKD.String(value))
	var out strings.Builder
	out.Grow(len(decomposed))
	for _, r := range decomposed {
		if folded, ok := searchFoldedLetters[r]; ok {
			out.WriteString(folded)
		} else if !searchDroppedMark(r) {
			out.WriteRune(r)
		}
	}
	return norm.NFC.String(out.String())
}

// searchDroppedMark is a diacritic mark search leaves out: a nonspacing
// mark of the Diacritic property, but the kana voicing marks.
func searchDroppedMark(r rune) bool {
	return r >= 0x300 && r != 0x3099 && r != 0x309A && unicode.Is(unicode.Mn, r) && unicode.Is(unicode.Diacritic, r)
}

// searchFoldedLetters are the lower-case letters no decomposition takes
// apart, as search reads them.
var searchFoldedLetters = map[rune]string{
	'ß': "ss", 'ς': "σ", 'æ': "ae", 'œ': "oe", 'þ': "th", 'ð': "d",
	'đ': "d", 'ħ': "h", 'ı': "i", 'ł': "l", 'ø': "o", 'ŧ': "t",
}

// searchPairedScript reports a letter of the scripts searched by pairs.
func searchPairedScript(r rune) bool {
	return unicode.In(r, unicode.Hangul, unicode.Han, unicode.Hiragana, unicode.Katakana,
		unicode.Thai, unicode.Lao, unicode.Khmer, unicode.Myanmar, unicode.Tibetan) ||
		// The prolonged sound marks belong to no script.
		r == 'ー' || r == 'ｰ'
}

// searchClusters splits text into characters, each with the marks after it.
func searchClusters(text string) []string {
	clusters := []string{}
	start := -1
	for index, r := range text {
		if start >= 0 && unicode.IsMark(r) {
			continue
		}
		if start >= 0 {
			clusters = append(clusters, text[start:index])
		}
		start = index
	}
	if start >= 0 {
		clusters = append(clusters, text[start:])
	}
	return clusters
}

func searchPairedCluster(cluster string) bool {
	r, _ := utf8.DecodeRuneInString(cluster)
	return searchPairedScript(r)
}

// searchTextGrams is what the index adds for folded text: each run of a
// paired script as its pairs and its last character, so every character
// starts an indexed word, and the Arabic and Hebrew words without the
// particles joined to their front. A run and a word end with their line, so
// the text's grams are its lines' in turn (searchPageGrams keeps a long
// page's lines').
func searchTextGrams(text string) string {
	grams := strings.Builder{}
	var run []string
	for line := range strings.SplitSeq(text, "\n") {
		run = searchLineGrams(&grams, line, run)
	}
	return strings.TrimSpace(grams.String())
}

// searchLineGrams writes a line's grams, each with a space after it, going
// through its characters as they come; run is room for a run's characters,
// given back for the next line.
func searchLineGrams(grams *strings.Builder, line string, run []string) []string {
	run = run[:0]
	flush := func() {
		for index := 0; index+1 < len(run); index++ {
			grams.WriteString(run[index])
			grams.WriteString(run[index+1])
			grams.WriteByte(' ')
		}
		if len(run) > 0 {
			grams.WriteString(run[len(run)-1])
			grams.WriteByte(' ')
		}
		run = run[:0]
	}
	cluster := func(text string) {
		if searchPairedCluster(text) {
			run = append(run, text)
			return
		}
		flush()
	}
	// Each character with the marks after it, as searchClusters splits them.
	start := -1
	for index, r := range line {
		if start >= 0 && unicode.IsMark(r) {
			continue
		}
		if start >= 0 {
			cluster(line[start:index])
		}
		start = index
	}
	if start >= 0 {
		cluster(line[start:])
	}
	flush()
	// The words as workspaceSearchWords finds them.
	for word := range strings.FieldsFuncSeq(line, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	}) {
		for _, stem := range searchWordStems(word) {
			grams.WriteString(stem)
			grams.WriteByte(' ')
		}
		for _, part := range searchWordParts(word) {
			grams.WriteString(part)
			grams.WriteByte(' ')
		}
	}
	return run
}

var (
	arabicConjunctions = []string{"", "و", "ف"}
	arabicPrepositions = []string{"", "ب", "ك", "ل"}
	hebrewPrefixes     = "הובלמשכ"
)

// searchWordParts are a word of letters and digits cut where they meet ("page12":
// page, 12), each a word of its own to Notion's search, which lists the pages
// holding one after those holding the whole (seen 2026-11-05); none for a word
// of either alone.
func searchWordParts(word string) []string {
	parts := []string{}
	start, letters := 0, false
	for index, r := range word {
		if unicode.IsMark(r) {
			continue
		}
		digit := unicode.IsDigit(r)
		if index > 0 && digit == letters {
			parts = append(parts, word[start:index])
			start = index
		}
		letters = !digit
	}
	if len(parts) == 0 {
		return nil
	}
	return append(parts, word[start:])
}

// searchWordStems are an Arabic-script or Hebrew word without the particles
// joined to its front: and, so, by, as, for, the (و ف ب ك ل ال; ל with ال
// is لل), and Hebrew's one-letter ones (ה ו ב ל מ ש כ), up to two of them.
// A stem keeps two letters at least.
func searchWordStems(word string) []string {
	first, _ := utf8.DecodeRuneInString(word)
	stems := []string{}
	add := func(stem string) {
		if utf8.RuneCountInString(stem) < 2 {
			return
		}
		for _, existing := range stems {
			if existing == stem {
				return
			}
		}
		stems = append(stems, stem)
	}
	switch {
	case unicode.Is(unicode.Arabic, first):
		for _, conjunction := range arabicConjunctions {
			for _, preposition := range arabicPrepositions {
				for _, article := range []string{"", "ال"} {
					prefix := conjunction + preposition + article
					if preposition == "ل" && article == "ال" {
						prefix = conjunction + "لل"
					}
					if prefix == "" || !strings.HasPrefix(word, prefix) {
						continue
					}
					rest := strings.TrimPrefix(word, prefix)
					add(rest)
					if article == "ال" && utf8.RuneCountInString(rest) >= 2 {
						add("ال" + rest)
					}
				}
			}
		}
	case unicode.Is(unicode.Hebrew, first):
		rest := word
		for strip := 0; strip < 2; strip++ {
			letter, size := utf8.DecodeRuneInString(rest)
			if !strings.ContainsRune(hebrewPrefixes, letter) {
				break
			}
			rest = rest[size:]
			add(rest)
		}
	}
	return stems
}

// searchWordTerm is a word's term in a search: the start of a word, for an
// English word also the words of its stem (englishStemForms), for a word of
// a paired script its pairs in a row, and for a number's word the word whole
// (searchNumberWord).
func searchWordTerm(word string) string {
	if searchNumberWord(word) {
		return `"` + word + `"`
	}
	clusters := searchClusters(word)
	for _, cluster := range clusters {
		if !searchPairedCluster(cluster) {
			if forms := englishStemForms(word); len(forms) > 0 {
				return `("` + word + `"* OR "` + strings.Join(forms, `" OR "`) + `")`
			}
			return `"` + word + `"*`
		}
	}
	if len(clusters) < 2 {
		return `"` + word + `"*`
	}
	pairs := make([]string, 0, len(clusters)-1)
	for index := 0; index+1 < len(clusters); index++ {
		pairs = append(pairs, clusters[index]+clusters[index+1])
	}
	// The start of a word too, while a page waits to be indexed in pairs.
	return `("` + word + `"* OR "` + strings.Join(pairs, " ") + `")`
}

// searchWordWholeTerm is a word's term held whole, as a page matching some
// keywords holds a word before the last typed (workspaceSearchKeywords.someWhole):
// the word, for an English word also the words of its stem, each whole, and
// for a number's word the word (searchWordTerm). A word of a paired script
// keeps its term.
func searchWordWholeTerm(word string) string {
	if searchPairedWord(word) {
		return searchWordTerm(word)
	}
	if forms := englishStemWholeForms(word); len(forms) > 0 {
		return `("` + word + `" OR "` + strings.Join(forms, `" OR "`) + `")`
	}
	return `"` + word + `"`
}

// searchNumberWord is whether a keyword is digits alone, a number's word,
// which Notion's search finds whole: "11/23" finds no "110/230", "1123" or
// "114-67 2383", "8123" no body holding "81234" (seen 2026-11-24 in a test
// workspace, two tabs). The last word typed is also the start of a word of
// a title, as the title is typed (workspaceSearchKeywords.TitleStart):
// "5123" finds the title "w nm 51234 mid", "5123 nm" doesn't, by 5123.
func searchNumberWord(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// searchWordStartTerm is a word's term as the start of a word alone.
func searchWordStartTerm(word string) string {
	return `"` + word + `"*`
}

// searchOrderedPairsTerm is an ordered term of searchWordTerm without the
// start of the word: its pairs alone. A row indexed in pairs — a page's
// chunks — holds them wherever it holds the word, and a start of a word reads
// every row holding a word of that start before it can say anything about one
// page (2026-10-19). A phrase in quotes is itself.
func searchOrderedPairsTerm(term string) string {
	start, pairs, found := strings.Cut(term, `"* OR "`)
	if !found || !strings.HasPrefix(start, `("`) || !strings.HasSuffix(pairs, `")`) {
		return term
	}
	return `"` + strings.TrimSuffix(pairs, ")")
}
