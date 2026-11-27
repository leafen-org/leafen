package main

import "strings"

// A question card's or an approval's notice comes with buttons (the server's
// agent_notice_actions.go): a card's choices or "allow" and "deny", and for a
// card of one question a reply in words of one's own. Pressing one sends it,
// with the token the notice carries for its person and its card alone, to
// the server the notice came from, as a browser's worker does
// (web/public/sw.js): a card no longer waited on says so in the notice's
// place, and an answer that can't go leaves the notice to open the chat.

// noticeAction is one of a notice's buttons.
type noticeAction struct {
	Action string `json:"action"`
	Title  string `json:"title"`
}

// noticeAnswer is where a notice's buttons answer and what they send.
type noticeAnswer struct {
	URL     string `json:"url"`
	User    string `json:"user"`
	Kind    string `json:"kind"`
	Session string `json:"session"`
	Target  string `json:"target"`
	Token   string `json:"token"`
	// What the notice says once the card is no longer waited on.
	Gone string `json:"gone"`
	// The reply's button and its send button, "" for a card that takes none.
	Reply string `json:"reply,omitempty"`
	Send  string `json:"send,omitempty"`
}

// noticeReplyAction is the reply's button.
const noticeReplyAction = "reply"

// noticeTakes says whether a notice's buttons send action: one of its
// buttons, or the reply with words in it.
func noticeTakes(actions []noticeAction, answer *noticeAnswer, action, text string) bool {
	if answer == nil {
		return false
	}
	if action == noticeReplyAction {
		return answer.Reply != "" && strings.TrimSpace(text) != ""
	}
	for _, button := range actions {
		if button.Action == action {
			return true
		}
	}
	return false
}

// noticeHasButtons says whether a notice shows buttons.
func noticeHasButtons(actions []noticeAction, answer *noticeAnswer) bool {
	return answer != nil && (len(actions) > 0 || answer.Reply != "")
}
