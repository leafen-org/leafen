//go:build ios || android

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A phone gets no push from Leafen (Leafen runs no push relay, decided
// 2026-10-19): while the app is open its window keeps the unread count, and
// when the system wakes the app for a background refresh the app asks the
// server its window last watched what came since, as the desktop app does
// while open, shows each as a notification and sets the count. The window
// hands over what it watched; the sign-in comes from the app's own secret
// storage. A server whose identity the window remembers (a Tailscale address)
// must show that identity on the connection the sign-in goes over.

// noticeWatch is what the window last watched on a phone.
type noticeWatch struct {
	Profile    string   `json:"profile"`
	Workspaces []string `json:"workspaces"`
	Through    int64    `json:"through"`
	// The server identity the window remembers there, "" for none.
	Identity string `json:"identity,omitempty"`
}

type phoneNotice struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url"`
	OccurredAt int64  `json:"occurredAt"`
	// The leafen: link a tap opens: the page on that server.
	Link string `json:"link"`
	// A question card's or an approval's buttons (notices_buttons.go), and
	// the server they answer on with the identity the window remembers there.
	Actions  []noticeAction `json:"actions,omitempty"`
	Answer   *noticeAnswer  `json:"answer,omitempty"`
	Profile  string         `json:"profile,omitempty"`
	Identity string         `json:"identity,omitempty"`
}

type phoneNotices struct {
	Notices     []phoneNotice `json:"notices"`
	UnreadCount int           `json:"unreadCount"`
	Through     int64         `json:"through"`
}

// noticeWorkspacesPerAsk is how many workspaces the server takes an ask.
const noticeWorkspacesPerAsk = 64

var errNoticeIdentity = errors.New("the server didn't show the identity the app remembers")

// fetchNotices asks the watched server for what came after the watch, oldest
// first, with the unread count and where to ask after next.
func fetchNotices(ctx context.Context, watch noticeWatch, token string) (phoneNotices, error) {
	profile, err := profileAccount(watch.Profile)
	if err != nil || token == "" || len(watch.Workspaces) == 0 {
		return phoneNotices{}, errors.New("nothing to ask")
	}
	answer := phoneNotices{Notices: []phoneNotice{}}
	for start := 0; start < len(watch.Workspaces); start += noticeWorkspacesPerAsk {
		query := url.Values{"workspaceId": watch.Workspaces[start:min(start+noticeWorkspacesPerAsk, len(watch.Workspaces))]}
		if watch.Through > 0 {
			query.Set("after", strconv.FormatInt(watch.Through, 10))
		}
		part := phoneNotices{}
		if err := askNotices(ctx, profile, watch.Identity, token, "/api/leafen/v1/account/desktop-notices?"+query.Encode(), &part); err != nil {
			return phoneNotices{}, err
		}
		answer.Notices = append(answer.Notices, part.Notices...)
		answer.UnreadCount += part.UnreadCount
		if start == 0 || part.Through < answer.Through {
			answer.Through = part.Through
		}
	}
	sort.SliceStable(answer.Notices, func(i, j int) bool { return answer.Notices[i].OccurredAt < answer.Notices[j].OccurredAt })
	for index := range answer.Notices {
		notice := &answer.Notices[index]
		notice.Link = "leafen://open?url=" + url.QueryEscape(profile+notice.URL)
		if noticeHasButtons(notice.Actions, notice.Answer) && strings.HasPrefix(notice.Answer.URL, "/api/") {
			notice.Profile, notice.Identity = watch.Profile, watch.Identity
		} else {
			notice.Actions, notice.Answer = nil, nil
		}
	}
	return answer, nil
}

// The ends of a notice's button pressed.
const (
	noticeAnswered = iota
	// The card is no longer waited on.
	noticeGone
	// The answer couldn't go.
	noticeUnsent
)

// answerNotice sends what a notice's button or reply says to the server the
// notice came from, over a connection showing the identity the window
// remembers there.
func answerNotice(ctx context.Context, notice phoneNotice, action, text string) int {
	answer := notice.Answer
	if !noticeTakes(notice.Actions, answer, action, text) || !strings.HasPrefix(answer.URL, "/api/") {
		return noticeUnsent
	}
	profile, err := profileAccount(notice.Profile)
	if err != nil {
		return noticeUnsent
	}
	body, err := json.Marshal(map[string]string{
		"user": answer.User, "kind": answer.Kind, "session": answer.Session, "target": answer.Target,
		"token": answer.Token, "action": action, "text": strings.TrimSpace(text),
	})
	if err != nil {
		return noticeUnsent
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, profile+answer.URL, bytes.NewReader(body))
	if err != nil {
		return noticeUnsent
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := askServer(profile, notice.Identity, request)
	if err != nil {
		return noticeUnsent
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
	switch response.StatusCode {
	case http.StatusOK:
		return noticeAnswered
	case http.StatusConflict:
		return noticeGone
	}
	return noticeUnsent
}

// noticeAfterAnswer is what a notice shows once its button was pressed: the
// card no longer waited on says so, an answer that couldn't go leaves the
// notice, without its buttons, to open the chat; nil once answered.
func noticeAfterAnswer(ctx context.Context, notice phoneNotice, action, text string) *phoneNotice {
	after := phoneNotice{ID: notice.ID, Title: notice.Title, Body: notice.Body, URL: notice.URL, Link: notice.Link}
	switch answerNotice(ctx, notice, action, text) {
	case noticeAnswered:
		return nil
	case noticeGone:
		after.Body = notice.Answer.Gone
	}
	return &after
}

func askNotices(ctx context.Context, profile, identity, token, path string, into *phoneNotices) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, profile+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := askServer(profile, identity, request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("the server answered " + response.Status)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(into)
}

// askServer sends the request to the server, over a connection showing the
// identity remembered there when there is one.
func askServer(profile, identity string, request *http.Request) (*http.Response, error) {
	if identity == "" {
		return (&http.Client{Timeout: 20 * time.Second}).Do(request)
	}
	return askVouched(profile, identity, request)
}

// askVouched sends the request over a connection whose certificate the
// remembered identity signed.
func askVouched(profile, identity string, request *http.Request) (*http.Response, error) {
	conn, reader, shown, err := vouchedConnection(profile, true)
	if err != nil {
		return nil, err
	}
	if shown != identity {
		conn.Close()
		return nil, errNoticeIdentity
	}
	request.Close = true
	if err := request.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		conn.Close()
		return nil, err
	}
	response.Body = closingBody{response.Body, conn}
	return response, nil
}

// closingBody closes the connection with the body.
type closingBody struct {
	io.ReadCloser
	conn io.Closer
}

func (body closingBody) Close() error {
	err := body.ReadCloser.Close()
	body.conn.Close()
	return err
}
