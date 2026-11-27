package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A person's profile answers with their newest activity and pages, a few of
// each, and how many there are in all; ?detail=full answers with every one,
// as the profile page reads it (2026-11-16, an agent calling every
// operation: one profile answered 25 KB, most of it the activity the
// profile page scrolls through).

// personProfileSummaryItems is how many of their activity and pages a
// profile's summary gives.
const personProfileSummaryItems = 10

// personProfileAnswer is a profile with how many of their activity and
// pages there are, and whether the lists hold them all.
type personProfileAnswer struct {
	personProfile
	ActivityCount int `json:"activityCount"`
	PageCount     int `json:"pageCount"`
	// Detail is full where the lists hold every one; a summary says how to
	// get them all.
	Detail string `json:"detail"`
}

// answerPersonProfile is a profile as the request asks for it.
func answerPersonProfile(event *core.RequestEvent, profile personProfile) personProfileAnswer {
	answer := personProfileAnswer{
		personProfile: profile, ActivityCount: len(profile.Activity), PageCount: len(profile.Pages), Detail: "full",
	}
	if strings.TrimSpace(event.Request.URL.Query().Get("detail")) != "full" {
		answer.Detail = "summary: the newest activity and pages; ?detail=full gives them all"
		answer.Activity = profile.Activity[:min(len(profile.Activity), personProfileSummaryItems)]
		answer.Pages = profile.Pages[:min(len(profile.Pages), personProfileSummaryItems)]
	}
	return answer
}
