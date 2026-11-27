package app

import (
	"slices"
	"strings"

	"github.com/kljensen/snowball/english"
)

// Search finds an English word by its stem too, as Notion's does (seen
// 2026-11-16 in ⌘K, two tabs: "runs" finds "run", "quarvels"
// "quarveling", "happiness" "happy", "hope" "hoping", "drelved"
// "drelvings", "glims" "glimming", "quendles" "quendling", "trombled" the
// line "the trombling stairs"; "runs" finds no "runway", nor "quarvels"
// "quarvelton", nor "vaskets" "vasketon"): a word whose stem is the
// keyword's, beside the words the keyword starts. The stem is English's
// Porter2 (Snowball), the rule the app's marks use (englishStem). The index
// keeps no stems (2026-10-14 decision: search language matching — an
// English word is matched by its start, the index no bigger): a keyword
// asks the index for its start and for the words of its stem a word can be
// written as (englishStemForms).

// englishStem is the stem of a word in lower case, English's Porter2 as the
// kljensen/snowball package gives it: "running" and "runs" are "run",
// "happiness" and "happy" "happi", "generalization" "general". The app's
// marks take the same stems (the web's porter2 package, englishStem.ts —
// the two agree on Snowball's 29,414 words and their stems and on every
// word of a 234,000-word English list, checked 2026-11-17).
func englishStem(word string) string {
	return english.Stem(word, true)
}

// searchEnglishWord is whether a folded keyword is an English word search
// finds by its stem: three letters or more, a to z alone.
func searchEnglishWord(word string) bool {
	if len(word) < 3 {
		return false
	}
	for index := 0; index < len(word); index++ {
		if word[index] < 'a' || word[index] > 'z' {
			return false
		}
	}
	return true
}

// searchEnglishStemEqual is whether a folded word of a text has the stem of
// an English keyword, the keyword's stem given.
func searchEnglishStemEqual(held, keyword, stem string) bool {
	// A stem keeps a word's first letter.
	return held != "" && held[0] == keyword[0] && searchEnglishWord(held) && englishStem(held) == stem
}

// englishStemFormEndings are endings a word of a stem may be written with,
// after the stem, the stem without its last letter, or with it doubled.
var englishStemFormEndings = []string{
	"", "s", "es", "e", "ed", "ing", "ings", "ly", "edly", "ingly", "er", "ers",
	"y", "ies", "ied", "ier", "iest", "ily", "iness", "ness", "ful", "fully", "fulness",
	"ment", "ments", "al", "ally", "ism", "ity", "ities", "ive", "ively", "iveness",
	"ous", "ously", "ousness", "ic", "ical", "ically", "ance", "ence", "ant", "ent",
	"able", "ably", "ible", "ion", "ions", "ate", "ated", "ates", "ating", "ation", "ations",
	"ational", "ize", "ized", "izes", "izing", "ization", "izations", "izer",
}

// englishStemFormLimit bounds the forms a keyword asks the index for.
const englishStemFormLimit = 24

// englishStemForms are the words of an English keyword's stem the keyword
// doesn't start, which its search asks the index for beside its start, each
// whole: "runs" asks "run" and "running", "happiness" "happy", "hope"
// "hoping". They are the usual ways of writing a stem, each checked to have
// it; an unusual one is found by its start alone.
func englishStemForms(word string) []string {
	return englishStemWordForms(word, false)
}

// englishStemWholeForms are the words of an English keyword's stem, those
// it starts too, which a keyword held only whole asks for beside itself
// (searchWordWholeTerm): "run" asks "runs" and "running".
func englishStemWholeForms(word string) []string {
	return englishStemWordForms(word, true)
}

// englishStemWordForms are the words of an English keyword's stem, those
// the keyword starts only when started.
func englishStemWordForms(word string, started bool) []string {
	if !searchEnglishWord(word) {
		return nil
	}
	stem := englishStem(word)
	roots := []string{stem}
	if n := len(stem); n > 1 {
		last := stem[n-1]
		roots = append(roots, stem[:n-1])
		if strings.IndexByte("aeiouy", last) < 0 {
			roots = append(roots, stem+string(last))
		}
	}
	forms := []string{}
	for _, root := range roots {
		for _, ending := range englishStemFormEndings {
			form := root + ending
			if form == word || !started && strings.HasPrefix(form, word) || slices.Contains(forms, form) || !searchEnglishWord(form) {
				continue
			}
			if englishStem(form) == stem {
				forms = append(forms, form)
				if len(forms) == englishStemFormLimit {
					return forms
				}
			}
		}
	}
	return forms
}

// searchEnglishStemRoot is the letters a folded keyword and the words of its
// stem it is asked by (englishStemForms) all hold, which a text holding one
// of them holds: "run" for "runs", "happ" for "happiness"; a word of no stem
// is itself.
func searchEnglishStemRoot(word string) string {
	root := word
	for _, form := range englishStemForms(word) {
		shared := 0
		for shared < len(root) && shared < len(form) && root[shared] == form[shared] {
			shared++
		}
		root = root[:shared]
	}
	if len(root) < 2 {
		return word
	}
	return root
}
