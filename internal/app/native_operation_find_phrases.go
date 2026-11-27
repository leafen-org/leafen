package app

import (
	"strings"
	"unicode"
)

// A keyword is matched as it is written, too (2026-11-26 review of every
// operation by its own keywords): the words a search keeps lose a keyword's
// joining words and endings, so "sign up" was sign and found every sign-in
// first, "blocked by" was block, "stats" was trimmed to the stem of state and
// status, "invite" tied with every invitation, and "like" and "me", joining
// words alone, found nothing. A query that holds a keyword of several words
// whole, word by word as written, weighs the text holding it
// nativeOperationFindPhraseBoost times; one word as written, only enough to
// go before those its trimmed form ties with (a word as common as token or
// template weighed more put the texts holding it before better finds); a
// keyword of joining words alone counts only for a query of them alone, so
// "I'd like to invite" never finds the reactions.

// What a whole keyword in the query weighs its text by: one of several words,
// and one word as written.
const (
	nativeOperationFindPhraseBoost = 1.5
	nativeOperationFindWordBoost   = 1.1
)

// nativeOperationFindTokens are a text's words as written, in lowercase, its
// joining words kept; a name's capitals start words.
func nativeOperationFindTokens(text string) []string {
	tokens := []string{}
	for _, field := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		tokens = append(tokens, nativeOperationHandlerWords(field)...)
	}
	return tokens
}

// nativeOperationFindPhrases are keywords as phrases of their written words.
func nativeOperationFindPhrases(keywords []string) [][]string {
	phrases := make([][]string, 0, len(keywords))
	for _, keyword := range keywords {
		if tokens := nativeOperationFindTokens(keyword); len(tokens) > 0 {
			phrases = append(phrases, tokens)
		}
	}
	return phrases
}

// phraseBoost is what the text weighs by for the keywords the query holds
// whole — for a query of joining words alone, a keyword of them alone, else
// one that holds another word: the most of them, or 1 for none.
func (text nativeOperationFindText) phraseBoost(finder nativeOperationFinder) float64 {
	boost := 1.0
	for _, phrase := range text.phrases {
		joiners := true
		for _, word := range phrase {
			joiners = joiners && nativeOperationFindStopWords[word]
		}
		if joiners != finder.joinersOnly || !nativeOperationFindHolds(finder.tokens, phrase) {
			continue
		}
		if len(phrase) > 1 {
			boost = max(boost, nativeOperationFindPhraseBoost)
		} else {
			boost = max(boost, nativeOperationFindWordBoost)
		}
	}
	return boost
}

// nativeOperationFindHolds is whether tokens hold phrase, word after word.
func nativeOperationFindHolds(tokens, phrase []string) bool {
	for start := 0; start+len(phrase) <= len(tokens); start++ {
		held := true
		for index, word := range phrase {
			held = held && tokens[start+index] == word
		}
		if held {
			return true
		}
	}
	return false
}
