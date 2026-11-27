package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A mention or a link Notion's API refuses for what it names (seen
// 2026-11-10), in Leafen's words but for the short "Invalid URL for link.",
// wherever rich text is written: a person it can't find, a
// template's mention outside a template, or a link to an address it doesn't
// take. Rich text in a block, a caption or a table cell says it after
// notionContentCreationPrefix; a title, a database
// page's text or a database's or data source's name or description says it
// alone, a person it can't find with 404.

const (
	notionTemplateMentionRefused = "A template mention goes only inside a template."
	notionTextLinkRefused        = "Invalid URL for link."
)

type notionMentionRefusal struct {
	// missingUser is the person's ID as written, when that is the fault.
	missingUser string
	// invalidLink is set when a link's address is the fault.
	invalidLink bool
}

func (refusal *notionMentionRefusal) said() string {
	switch {
	case refusal.missingUser != "":
		return notionUserNotFoundMessage(refusal.missingUser)
	case refusal.invalidLink:
		return notionTextLinkRefused
	}
	return notionTemplateMentionRefused
}

// Error is the refusal as a block's content says it.
func (refusal *notionMentionRefusal) Error() string {
	return notionContentCreationPrefix + refusal.said()
}

// Unwrap is the input error a block write answers with Error's words.
func (refusal *notionMentionRefusal) Unwrap() error {
	return graphInputError(errorCodeGraphReferenceInvalid, refusal.Error())
}

// notionMentionRefusedAlone is err as a title, a database page's text or a
// name says it, when err is a mention's or a link's refusal: a person it
// can't find as notionDatabaseRowPageUserMissing, any other as a request
// error.
func notionMentionRefusedAlone(err error) (error, bool) {
	refusal := &notionMentionRefusal{}
	if !errors.As(err, &refusal) {
		return err, false
	}
	if refusal.missingUser != "" {
		return &notionDatabaseRowPageUserMissing{id: refusal.missingUser}, true
	}
	return notionDatabaseRowPageRequestInvalid("%s", refusal.said()), true
}

// notionUserMissingAnswer answers a person a request names who can't be
// found, as Notion's API does.
func notionUserMissingAnswer(event *core.RequestEvent, err error) (error, bool) {
	missingUser := &notionDatabaseRowPageUserMissing{}
	if !errors.As(err, &missingUser) {
		return nil, false
	}
	return notionCompatibilityError(event, http.StatusNotFound, notionErrorObjectNotFound, notionUserNotFoundMessage(missingUser.id)), true
}
