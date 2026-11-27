//go:build ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework BackgroundTasks -framework UserNotifications -framework UIKit -framework Foundation
#include <stdlib.h>

int leafenNoticesAllowed(int ask);
void leafenNoticeShow(const char *identifier, const char *title, const char *body, const char *link, const char *buttons);
void leafenNoticeBadge(int count);
*/
import "C"

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// DesktopNotices on an iPhone or iPad (notices_fetch.go): the window keeps
// the unread count on the icon while the app is open and says what it
// watched; a background refresh (notices_ios.m) shows what came since.
type DesktopNotices struct {
	window *application.WebviewWindow
}

func newDesktopNotices() *DesktopNotices { return &DesktopNotices{} }

func (n *DesktopNotices) services() []application.Service { return nil }

// Allowed says whether iOS lets the app show notifications.
func (n *DesktopNotices) Allowed() (bool, error) { return C.leafenNoticesAllowed(0) == 1, nil }

// Allow asks the person once to let the app show notifications and badges.
func (n *DesktopNotices) Allow() (bool, error) { return C.leafenNoticesAllowed(1) == 1, nil }

// Show shows a notification now; link is the leafen: link a tap opens. The
// window shows none on a phone: the app's own inbox shows what comes while
// it is open, and the background refresh's notices have the buttons.
func (n *DesktopNotices) Show(id, title, body, link string, actions []noticeAction, answer *noticeAnswer) error {
	showNotice(phoneNotice{ID: id, Title: title, Body: body, Link: link})
	return nil
}

// SetUnread sets the count on the app's icon.
func (n *DesktopNotices) SetUnread(count int) error {
	C.leafenNoticeBadge(C.int(max(count, 0)))
	return nil
}

// Watch keeps what the window watched — the server, its workspaces, what it
// has seen up to and the identity it remembers there — for the background
// refresh; an empty profile stops it.
func (n *DesktopNotices) Watch(profile string, workspaces []string, through int64, identity string) error {
	noticeWatchMu.Lock()
	defer noticeWatchMu.Unlock()
	if profile == "" || len(workspaces) == 0 {
		err := os.Remove(noticeWatchPath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := profileAccount(profile); err != nil {
		return err
	}
	return writeNoticeWatch(noticeWatch{Profile: profile, Workspaces: workspaces, Through: through, Identity: identity})
}

var noticeWatchMu sync.Mutex

func noticeWatchPath() string {
	base := application.Mobile.StoragePath()
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "notices-watch.json")
}

func writeNoticeWatch(watch noticeWatch) error {
	data, err := json.Marshal(watch)
	if err != nil {
		return err
	}
	path := noticeWatchPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// showNotice shows one notification; a question card's or an approval's
// keeps itself in it (as JSON) for its buttons.
func showNotice(notice phoneNotice) {
	buttons := ""
	if noticeHasButtons(notice.Actions, notice.Answer) {
		if data, err := json.Marshal(notice); err == nil {
			buttons = string(data)
		}
	}
	identifier, title, body, link := C.CString(notice.ID), C.CString(notice.Title), C.CString(notice.Body), C.CString(notice.Link)
	kept := C.CString(buttons)
	defer C.free(unsafe.Pointer(identifier))
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(body))
	defer C.free(unsafe.Pointer(link))
	defer C.free(unsafe.Pointer(kept))
	C.leafenNoticeShow(identifier, title, body, link, kept)
}

// leafenNoticeAnswer takes a notice's button pressed (notices_ios.m): it
// answers on the notice's server, and the notice says so when the card is
// no longer waited on, or comes back to open the chat when the answer
// couldn't go.
//
//export leafenNoticeAnswer
func leafenNoticeAnswer(kept, action, text *C.char) {
	notice := phoneNotice{}
	if json.Unmarshal([]byte(C.GoString(kept)), &notice) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if after := noticeAfterAnswer(ctx, notice, C.GoString(action), C.GoString(text)); after != nil {
		showNotice(*after)
	}
}

// leafenRefreshNotices is iOS's background refresh: what came since the
// window's watch is shown and the count set, within iOS's half minute.
//
//export leafenRefreshNotices
func leafenRefreshNotices() {
	noticeWatchMu.Lock()
	defer noticeWatchMu.Unlock()
	data, err := os.ReadFile(noticeWatchPath())
	watch := noticeWatch{}
	if err != nil || json.Unmarshal(data, &watch) != nil || watch.Profile == "" {
		return
	}
	account, err := profileAccount(watch.Profile)
	store := platformSecretStore()
	if err != nil || store == nil {
		return
	}
	secret, found, err := store.load(account)
	session := struct {
		Token string `json:"token"`
	}{}
	if err != nil || !found || json.Unmarshal([]byte(secret), &session) != nil || session.Token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	answer, err := fetchNotices(ctx, watch, session.Token)
	if err != nil {
		return
	}
	for _, notice := range answer.Notices {
		showNotice(notice)
	}
	C.leafenNoticeBadge(C.int(answer.UnreadCount))
	watch.Through = answer.Through
	_ = writeNoticeWatch(watch)
}
