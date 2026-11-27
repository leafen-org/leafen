//go:build android

package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// DesktopNotices on Android: the app's own Java side shows notifications
// and refreshes them in the background (LeafenNotices.java, reached by the
// window as window.leafenNotices), so the window doesn't come here.
type DesktopNotices struct {
	window *application.WebviewWindow
}

var errNoDesktopNotices = errors.New("Android shows notifications through the app's own Java side")

func newDesktopNotices() *DesktopNotices { return &DesktopNotices{} }

func (n *DesktopNotices) services() []application.Service { return nil }

func (n *DesktopNotices) Allowed() (bool, error) { return false, nil }

func (n *DesktopNotices) Allow() (bool, error) { return false, errNoDesktopNotices }

func (n *DesktopNotices) Show(id, title, body, url string, actions []noticeAction, answer *noticeAnswer) error {
	return errNoDesktopNotices
}

func (n *DesktopNotices) SetUnread(count int) error { return nil }

// leafenAndroidFetchNotices is the background job's ask (LeafenNoticeJob.java
// through notices_android.c): the watch as the window gave it and the
// sign-in's token in, what came since — or {"error"} — out as JSON, which the
// caller frees.
//
//export leafenAndroidFetchNotices
func leafenAndroidFetchNotices(watchJSON, token *C.char) *C.char {
	watch := noticeWatch{}
	answer := any(map[string]string{"error": "the watch can't be read"})
	if json.Unmarshal([]byte(C.GoString(watchJSON)), &watch) == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if notices, err := fetchNotices(ctx, watch, C.GoString(token)); err != nil {
			answer = map[string]string{"error": err.Error()}
		} else {
			answer = notices
		}
	}
	data, _ := json.Marshal(answer)
	return C.CString(string(data))
}

// leafenAndroidAnswerNotice is a notice's button or reply pressed
// (LeafenNoticeAnswer.java through notices_android.c): the notice as the job
// showed it, the button and the words written in a reply in; {"notice"} — what
// the notice shows now — or {} once answered, out as JSON, which the caller
// frees. A broadcast's few seconds bound the ask.
//
//export leafenAndroidAnswerNotice
func leafenAndroidAnswerNotice(noticeJSON, action, text *C.char) *C.char {
	notice := phoneNotice{}
	answer := map[string]any{}
	if json.Unmarshal([]byte(C.GoString(noticeJSON)), &notice) == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if after := noticeAfterAnswer(ctx, notice, C.GoString(action), C.GoString(text)); after != nil {
			answer["notice"] = after
		}
	}
	data, _ := json.Marshal(answer)
	return C.CString(string(data))
}
