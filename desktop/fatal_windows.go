package main

import (
	"strings"

	"golang.org/x/sys/windows"
)

// The Microsoft page that offers the WebView2 runtime the window draws with.
const webviewRuntimeURL = "https://developer.microsoft.com/microsoft-edge/webview2/"

// The message box's answer when the person chooses OK.
const messageBoxOK = 1

// reportFatal tells the person why Leafen is closing before it closes. Without
// the WebView2 runtime the window can't open, and the app otherwise ends with
// no window and no word (Windows 11 arm64 with the runtime out of reach,
// 2026-10-02); the person can open the runtime's page from the message.
func reportFatal(err error) {
	korean := false
	if languages, languageErr := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); languageErr == nil && len(languages) > 0 {
		korean = strings.HasPrefix(strings.ToLower(languages[0]), "ko")
	}
	if !webviewRuntimeMissing(err) {
		message := "Leafen couldn't open its window: " + err.Error()
		if korean {
			message = "리펜 창을 열지 못했습니다: " + err.Error()
		}
		showMessage(message, windows.MB_OK|windows.MB_ICONERROR)
		return
	}
	message := "Leafen needs the Microsoft Edge WebView2 Runtime to open its window. Choose OK to open the page " +
		"that offers it, install it and open Leafen again."
	if korean {
		message = "리펜 창을 열려면 Microsoft Edge WebView2 런타임이 필요합니다. 확인을 누르면 런타임을 다운로드하는 페이지가 열립니다. " +
			"설치한 뒤 리펜을 다시 여세요."
	}
	if showMessage(message, windows.MB_OKCANCEL|windows.MB_ICONWARNING) == messageBoxOK {
		_ = windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(webviewRuntimeURL), nil, nil, windows.SW_SHOWNORMAL)
	}
}

func showMessage(message string, flags uint32) int32 {
	choice, _ := windows.MessageBox(0, windows.StringToUTF16Ptr(message), windows.StringToUTF16Ptr("Leafen"), flags|windows.MB_SETFOREGROUND)
	return choice
}
