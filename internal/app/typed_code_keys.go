package app

import "strings"

// A code typed with a Korean keyboard on comes in as the Hangul its keys make
// on the standard (2-beolsik) layout: "1JC9-SGPK" as "1ㅓㅊ9-ㄴ혜ㅏ" (2026-10-27,
// a phone whose keyboard was Korean, refused as a wrong code). typedCodeKeys
// reads such text back as the keys pressed, for every field a person copies a
// code into: a plain key as its small letter, a shifted one (ㅃ ㅉ ㄸ ㄲ ㅆ ㅒ ㅖ)
// as its capital, a syllable or a double jamo as the keys that made it.
// Anything else stays as typed.
var koreanKeyOf = map[rune]string{
	'ㅂ': "q", 'ㅈ': "w", 'ㄷ': "e", 'ㄱ': "r", 'ㅅ': "t", 'ㅛ': "y", 'ㅕ': "u", 'ㅑ': "i", 'ㅐ': "o", 'ㅔ': "p",
	'ㅁ': "a", 'ㄴ': "s", 'ㅇ': "d", 'ㄹ': "f", 'ㅎ': "g", 'ㅗ': "h", 'ㅓ': "j", 'ㅏ': "k", 'ㅣ': "l",
	'ㅋ': "z", 'ㅌ': "x", 'ㅊ': "c", 'ㅍ': "v", 'ㅠ': "b", 'ㅜ': "n", 'ㅡ': "m",
	'ㅃ': "Q", 'ㅉ': "W", 'ㄸ': "E", 'ㄲ': "R", 'ㅆ': "T", 'ㅒ': "O", 'ㅖ': "P",
	'ㅘ': "hk", 'ㅙ': "ho", 'ㅚ': "hl", 'ㅝ': "nj", 'ㅞ': "np", 'ㅟ': "nl", 'ㅢ': "ml",
	'ㄳ': "rt", 'ㄵ': "sw", 'ㄶ': "sg", 'ㄺ': "fr", 'ㄻ': "fa", 'ㄼ': "fq", 'ㄽ': "ft", 'ㄾ': "fx", 'ㄿ': "fv", 'ㅀ': "fg", 'ㅄ': "qt",
}

// The jamo a syllable is composed of, in Unicode's order; a syllable without
// a final consonant has the first, empty, final.
var (
	syllableInitials = []rune("ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ")
	syllableVowels   = []rune("ㅏㅐㅑㅒㅓㅔㅕㅖㅗㅘㅙㅚㅛㅜㅝㅞㅟㅠㅡㅢㅣ")
	syllableFinals   = []rune(" ㄱㄲㄳㄴㄵㄶㄷㄹㄺㄻㄼㄽㄾㄿㅀㅁㅂㅄㅅㅆㅇㅈㅊㅋㅌㅍㅎ")
)

const (
	firstHangulSyllable = 0xAC00
	lastHangulSyllable  = 0xD7A3
)

func typedCodeKeys(typed string) string {
	var keys strings.Builder
	for _, r := range typed {
		if r >= firstHangulSyllable && r <= lastHangulSyllable {
			index := int(r - firstHangulSyllable)
			keys.WriteString(koreanKeyOf[syllableInitials[index/(len(syllableVowels)*len(syllableFinals))]])
			keys.WriteString(koreanKeyOf[syllableVowels[index/len(syllableFinals)%len(syllableVowels)]])
			if final := index % len(syllableFinals); final > 0 {
				keys.WriteString(koreanKeyOf[syllableFinals[final]])
			}
			continue
		}
		if key, ok := koreanKeyOf[r]; ok {
			keys.WriteString(key)
			continue
		}
		keys.WriteRune(r)
	}
	return keys.String()
}
