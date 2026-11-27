//go:build (darwin && !ios) || windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// openNoticeEvent tells the window to open the page of a notification the
// person clicked.
const openNoticeEvent = "leafen:open-notice"

// noticeActionEvent tells the window a notification's button or reply was
// pressed; the window answers on its server (notices_buttons.go).
const noticeActionEvent = "leafen:notice-action"

// noticeReplied is the action the system's reply comes as.
const noticeReplied = "TEXT_REPLY"

// noticeCategoriesKept is how many notices' buttons stay registered, the
// newest, so what the system keeps of them doesn't grow.
const noticeCategoriesKept = 16

// noticesShownKept is how many notices shown are remembered, the newest, so
// one is shown once however many of the app's tabs pass it on.
const noticesShownKept = 200

// DesktopNotices shows the person's comments and mentions as the system's
// notifications while the app is open, as Notion's desktop app does, and
// the unread count on the app's icon (the Dock on macOS, the taskbar on
// Windows). The window has no push service: it asks its server for what is
// new and tells this what to show.
type DesktopNotices struct {
	notifier *notifications.NotificationService
	badge    *dock.DockService
	window   *application.WebviewWindow

	categoriesMu sync.Mutex
	// shown are the notices shown lately, oldest first.
	shownMu sync.Mutex
	shown   []string
}

func newDesktopNotices() *DesktopNotices {
	notices := &DesktopNotices{notifier: notifications.New(), badge: dock.New()}
	// Clicking a notification brings the window forward on its page; a
	// button answers there without it.
	notices.notifier.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			return
		}
		info := result.Response.UserInfo
		url, _ := info["url"].(string)
		action := result.Response.ActionIdentifier
		if answer, _ := info["answer"].(string); answer != "" && action != "" && action != notifications.DefaultActionIdentifier {
			if action == noticeReplied {
				action = noticeReplyAction
			}
			title, _ := info["title"].(string)
			body, _ := info["body"].(string)
			application.Get().Event.Emit(noticeActionEvent, map[string]string{
				"id": result.Response.ID, "title": title, "body": body, "url": url,
				"answer": answer, "action": action, "text": result.Response.UserText,
			})
			return
		}
		if !showFront() && notices.window != nil {
			notices.window.Show()
			notices.window.Focus()
		}
		// The page in front opens it (the shown tab on a Mac), named so the
		// app's other pages leave it.
		if url != "" {
			target := ""
			if page := frontPage(); page != nil {
				target = page.Name()
			}
			application.Get().Event.Emit(openNoticeEvent, []string{url, target})
		}
	})
	return notices
}

// services are the system's notification and badge services, started with
// the app beside this one.
func (n *DesktopNotices) services() []application.Service {
	return []application.Service{application.NewService(n.notifier), application.NewService(n.badge)}
}

// Allowed says whether the system lets the app show notifications.
func (n *DesktopNotices) Allowed() (bool, error) {
	return n.notifier.CheckNotificationAuthorization()
}

// Allow asks the system to let the app show notifications; macOS asks the
// person once.
func (n *DesktopNotices) Allow() (bool, error) {
	return n.notifier.RequestNotificationAuthorization()
}

// Show shows one notification; clicking it opens url in the window. A
// question card's or an approval's has its buttons and, for a card of one
// question, a reply (macOS and Windows), which the window sends.
func (n *DesktopNotices) Show(id, title, body, url string, actions []noticeAction, answer *noticeAnswer) error {
	// The app's tabs ask their server one for all, and two starting at once
	// may both pass one on: it shows once.
	if !n.firstShow(id) {
		return nil
	}
	// No thread: Windows turns one into a toast header, which shows the
	// toast as only "new notification" (seen 2026-10-03).
	options := notifications.NotificationOptions{ID: id, Title: title, Body: body, Data: map[string]interface{}{"url": url}}
	if !noticeHasButtons(actions, answer) {
		return n.notifier.SendNotification(options)
	}
	kept, err := json.Marshal(answer)
	category := ""
	if err == nil {
		category, err = n.keepCategory(id, actions, answer)
	}
	if err != nil {
		return n.notifier.SendNotification(options)
	}
	options.CategoryID = category
	options.Data["answer"], options.Data["title"], options.Data["body"] = string(kept), title, body
	return n.notifier.SendNotificationWithActions(options)
}

// firstShow notes a notice as shown, and says whether it wasn't yet.
func (n *DesktopNotices) firstShow(id string) bool {
	n.shownMu.Lock()
	defer n.shownMu.Unlock()
	if slices.Contains(n.shown, id) {
		return false
	}
	n.shown = append(n.shown, id)
	if len(n.shown) > noticesShownKept {
		n.shown = n.shown[len(n.shown)-noticesShownKept:]
	}
	return true
}

// keepCategory registers a notice's buttons as its own category, keeping the
// newest few and letting the older go.
func (n *DesktopNotices) keepCategory(id string, actions []noticeAction, answer *noticeAnswer) (string, error) {
	n.categoriesMu.Lock()
	defer n.categoriesMu.Unlock()
	category := notifications.NotificationCategory{ID: "leafen-notice-" + id, HasReplyField: answer.Reply != ""}
	for _, action := range actions {
		category.Actions = append(category.Actions, notifications.NotificationAction{ID: action.Action, Title: action.Title})
	}
	if category.HasReplyField {
		// macOS names the reply's button and its send button the same;
		// Windows writes in a box and sends with a button.
		category.ReplyButtonTitle = answer.Reply
		if runtime.GOOS == "windows" && answer.Send != "" {
			category.ReplyPlaceholder, category.ReplyButtonTitle = answer.Reply, answer.Send
		}
	}
	if err := n.notifier.RegisterNotificationCategory(category); err != nil {
		return "", err
	}
	path := noticeCategoriesPath()
	kept := []string{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &kept)
	}
	kept = append(slices.DeleteFunc(kept, func(one string) bool { return one == category.ID }), category.ID)
	for len(kept) > noticeCategoriesKept {
		_ = n.notifier.RemoveNotificationCategory(kept[0])
		kept = kept[1:]
	}
	if data, err := json.Marshal(kept); err == nil && path != "" {
		_ = os.WriteFile(path, data, 0o600)
	}
	return category.ID, nil
}

// noticeCategoriesPath is where the registered notices' categories are
// listed, beside the app's local workspace.
func noticeCategoriesPath() string {
	dir, err := localDataDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "notice-categories.json")
}

// SetUnread shows the unread count on the app's icon, none at zero.
func (n *DesktopNotices) SetUnread(count int) error {
	if count <= 0 {
		return n.badge.RemoveBadge()
	}
	label := strconv.Itoa(count)
	if count > 99 {
		label = "99+"
	}
	return n.badge.SetBadge(label)
}
