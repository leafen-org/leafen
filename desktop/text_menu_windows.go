//go:build windows

package main

import (
	"context"
	"errors"
	"unicode/utf16"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// TextMenu is what the Windows app's page asks of the app for the right
// press's menu it draws itself (web/src/lib/desktopTextMenu.ts), as Notion
// 7.44.1's Windows app's page asks Electron (measured beside it 2026-11-24:
// a misspelt word's five corrections, "사전에 추가" and "맞춤법 검사
// 비활성화"; "잘라내기 Ctrl+X", "복사 Ctrl+C", and "붙여넣기" only with
// something to paste): a word's spelling by Windows' own spell checker (its
// ISpellChecker, in each language the person reads), the dictionary, and the
// page's own cut, copy and paste. WebView2's own menu never shows (the page
// takes every right press). Each runs on the main thread, where COM is.
type TextMenu struct{}

// TextMenuSpelling is a word's spelling: whether spelling is checked as one
// types, whether the word is misspelt, and its corrections; and whether the
// clipboard holds anything to paste.
type TextMenuSpelling struct {
	Checking bool     `json:"checking"`
	Misspelt bool     `json:"misspelt"`
	Guesses  []string `json:"guesses"`
	Paste    bool     `json:"paste"`
}

// textMenuChecking says spelling is checked as one types; "맞춤법 검사
// 비활성화" turns it off in every tab's page until it is turned on again.
var textMenuChecking = true

// The corrections a misspelt word's menu offers at most, as Notion's.
const maxSpellingGuesses = 5

// Windows' spell checker (spellcheck.h).
var (
	clsidSpellCheckerFactory = windows.GUID{Data1: 0x7ab36653, Data2: 0x1796, Data3: 0x484b, Data4: [8]byte{0xbd, 0xfa, 0xe7, 0x4f, 0x1d, 0xb7, 0xc1, 0xdc}}
	iidSpellCheckerFactory   = windows.GUID{Data1: 0x8e018a9d, Data2: 0x2415, Data3: 0x4677, Data4: [8]byte{0xbf, 0x08, 0x79, 0x4e, 0xa6, 0x1f, 0x94, 0xbb}}
	procCoCreateInstance     = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")
	procIsClipboardFormat    = windows.NewLazySystemDLL("user32.dll").NewProc("IsClipboardFormatAvailable")
)

// Places in the method tables of spellcheck.h and objidl.h.
const (
	factoryIsSupported       = 4
	factoryCreateSpellCheck  = 5
	checkerCheck             = 4
	checkerSuggest           = 5
	checkerAdd               = 6
	enumNext                 = 3
	clipboardUnicodeText     = 13
	clipboardBitmapAvailable = 2
)

// spellCheckers are a checker for each language the person reads that
// Windows can check, made once.
var spellCheckers []unsafe.Pointer

func wide(text string) *uint16 {
	pointer, _ := windows.UTF16PtrFromString(text)
	return pointer
}

func makeSpellCheckers() []unsafe.Pointer {
	if spellCheckers != nil {
		return spellCheckers
	}
	spellCheckers = []unsafe.Pointer{}
	var factory unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidSpellCheckerFactory)), 0, uintptr(windows.CLSCTX_INPROC_SERVER),
		uintptr(unsafe.Pointer(&iidSpellCheckerFactory)), uintptr(unsafe.Pointer(&factory)))
	if hr != 0 || factory == nil {
		return spellCheckers
	}
	defer comRelease(factory)
	languages, _ := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	seen := map[string]bool{}
	for _, language := range append(languages, "en-US") {
		if seen[language] {
			continue
		}
		seen[language] = true
		var supported int32
		if comCall(factory, factoryIsSupported, uintptr(unsafe.Pointer(wide(language))), uintptr(unsafe.Pointer(&supported))) != 0 || supported == 0 {
			continue
		}
		var checker unsafe.Pointer
		if comCall(factory, factoryCreateSpellCheck, uintptr(unsafe.Pointer(wide(language))), uintptr(unsafe.Pointer(&checker))) == 0 && checker != nil {
			spellCheckers = append(spellCheckers, checker)
		}
	}
	return spellCheckers
}

// misspelt says whether a checker finds the word wrong.
func misspelt(checker unsafe.Pointer, word string) bool {
	var mistakes unsafe.Pointer
	if comCall(checker, checkerCheck, uintptr(unsafe.Pointer(wide(word))), uintptr(unsafe.Pointer(&mistakes))) != 0 || mistakes == nil {
		return false
	}
	defer comRelease(mistakes)
	var found unsafe.Pointer
	if comCall(mistakes, enumNext, uintptr(unsafe.Pointer(&found))) != 0 || found == nil {
		return false
	}
	comRelease(found)
	return true
}

// guesses are a checker's corrections for a word.
func guesses(checker unsafe.Pointer, word string) []string {
	var list unsafe.Pointer
	if comCall(checker, checkerSuggest, uintptr(unsafe.Pointer(wide(word))), uintptr(unsafe.Pointer(&list))) != 0 || list == nil {
		return nil
	}
	defer comRelease(list)
	var out []string
	for len(out) < maxSpellingGuesses {
		var text *uint16
		var fetched uint32
		if comCall(list, enumNext, 1, uintptr(unsafe.Pointer(&text)), uintptr(unsafe.Pointer(&fetched))) != 0 || fetched == 0 || text == nil {
			break
		}
		out = append(out, windows.UTF16PtrToString(text))
		windows.CoTaskMemFree(unsafe.Pointer(text))
	}
	return out
}

// Spelling says whether spelling is checked and whether the word at
// [at, at+length) of text (UTF-16 units) is misspelt — in every language
// the person reads, as Chromium's checker says — with its corrections, and
// whether there is anything to paste.
func (*TextMenu) Spelling(text string, at, length int) TextMenuSpelling {
	return application.InvokeSyncWithResult(func() TextMenuSpelling {
		answer := TextMenuSpelling{Checking: textMenuChecking, Guesses: []string{}, Paste: canPaste()}
		units := utf16.Encode([]rune(text))
		if !textMenuChecking || length <= 0 || at < 0 || at+length > len(units) {
			return answer
		}
		word := string(utf16.Decode(units[at : at+length]))
		checkers := makeSpellCheckers()
		if len(checkers) == 0 {
			return answer
		}
		for _, checker := range checkers {
			if !misspelt(checker, word) {
				return answer
			}
		}
		answer.Misspelt = true
		for _, checker := range checkers {
			for _, guess := range guesses(checker, word) {
				if len(answer.Guesses) < maxSpellingGuesses && !containsString(answer.Guesses, guess) {
					answer.Guesses = append(answer.Guesses, guess)
				}
			}
		}
		return answer
	})
}

func containsString(list []string, text string) bool {
	for _, each := range list {
		if each == text {
			return true
		}
	}
	return false
}

// canPaste says the clipboard holds text or a picture.
func canPaste() bool {
	text, _, _ := procIsClipboardFormat.Call(clipboardUnicodeText)
	picture, _, _ := procIsClipboardFormat.Call(clipboardBitmapAvailable)
	return text != 0 || picture != 0
}

// Learn adds a word to Windows' dictionary ("사전에 추가"), for the first
// language the person reads.
func (*TextMenu) Learn(word string) {
	application.InvokeSync(func() {
		if checkers := makeSpellCheckers(); len(checkers) > 0 && word != "" {
			comCall(checkers[0], checkerAdd, uintptr(unsafe.Pointer(wide(word))))
		}
	})
}

// Edit cuts, copies or pastes in the asking page as Ctrl+X, Ctrl+C and
// Ctrl+V do — the page's own way, its paste too — or turns spelling as one
// types off or on in every tab's page ("spelling").
func (*TextMenu) Edit(ctx context.Context, action string) error {
	window, _ := ctx.Value(application.WindowKey).(application.Window)
	if window == nil {
		return errors.New("no window asked")
	}
	keys := map[string]uint16{"cut": 'X', "copy": 'C', "paste": 'V'}
	application.InvokeSync(func() {
		if action == "spelling" {
			textMenuChecking = !textMenuChecking
			for _, tab := range desktopTabs.allTabs() {
				if tab.window != nil {
					tab.window.EmitEvent(spellcheckEvent, map[string]any{"window": tab.window.Name(), "on": textMenuChecking})
				}
			}
			return
		}
		if key, ok := keys[action]; ok {
			sendChord(w32.VK_CONTROL, key)
		}
	})
	return nil
}

// spellcheckEvent tells a page whether spelling is checked as one types.
const spellcheckEvent = "leafen:spellcheck"

// textMenuServices: the page's right-press menu asks the app.
func textMenuServices() []application.Service {
	return []application.Service{application.NewService(&TextMenu{})}
}
