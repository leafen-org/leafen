package app

import "strings"

// validNotionCodeLanguage reports a language Notion's API takes: the code
// block languages (contracts/code-languages.json), "plain text",
// "java/c/c++/c#" and "abc", which Notion's code block doesn't list but its
// API takes and keeps (seen 2026-11-10).
func validNotionCodeLanguage(language string) bool {
	if language == plainTextCodeLanguage || language == "java/c/c++/c#" || language == "abc" {
		return true
	}
	_, ok := codeLanguageValues[language]
	return ok
}

// canonicalNotionCodeLanguage is the language Notion keeps for a valid
// one: it takes "java/c/c++/c#" and keeps "java" (seen 2026-11-01).
func canonicalNotionCodeLanguage(language string) string {
	if language == "java/c/c++/c#" {
		return "java"
	}
	return language
}

// importedCodeLanguage is the language an imported Markdown fence names, as
// Notion's Markdown import reads it (seen 2026-11-01): the whole name in any
// case, a language's own ("C++", "objective-c", "Makefile") or a short name
// ("js", "tsx", "dockerfile"); anything else — "sh-session", "python3",
// "vb.net", "webassembly" — is plain text (""). A paste reads only the name's
// first word, as written (web/src/components/editor/codeLanguages.ts).
func importedCodeLanguage(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if language, ok := codeLanguageShortNames[name]; ok {
		return language
	}
	if _, unread := unreadCodeLanguageNames[name]; unread {
		return ""
	}
	if _, ok := codeLanguageValues[name]; ok {
		return name
	}
	return ""
}
