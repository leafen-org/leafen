package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A template's body may hold template mentions, which take their value when
// the template makes a page, as Notion's "when duplicated" mentions do: today
// becomes that day's date, now that moment, and me the person who used the
// template, in the time zone the request names or else on that person's
// clock (person_clock.go): an agent's page made from a template at 03:00 in
// Seoul took UTC's yesterday for @today (2026-11-26). Its date properties may
// hold "@today" or "@now" and its person properties "@me" among their
// people, which take their value the same way. A template copied as a
// template keeps them.

const (
	graphTemplateMention      = "template"
	graphTemplateMentionToday = "today"
	graphTemplateMentionNow   = "now"
	graphTemplateMentionMe    = "me"
)

func validGraphTemplateMention(value string) bool {
	return value == graphTemplateMentionToday || value == graphTemplateMentionNow || value == graphTemplateMentionMe
}

// databaseTemplateUse is who uses a template, when and where.
type databaseTemplateUse struct {
	ActorID   string
	ActorName string
	At        time.Time
	Location  *time.Location
}

// newDatabaseTemplateUse names the use of a template by the actor now, in
// the named time zone, an empty one on the actor's clock.
func newDatabaseTemplateUse(app core.App, actorID, timeZone string) (databaseTemplateUse, error) {
	location := personLocation(app, actorID)
	if timeZone != "" {
		loaded, err := time.LoadLocation(timeZone)
		if err != nil {
			return databaseTemplateUse{}, err
		}
		location = loaded
	}
	use := databaseTemplateUse{ActorID: actorID, At: time.Now(), Location: location}
	if actor, err := app.FindRecordById("users", actorID); err == nil {
		use.ActorName = strings.TrimSpace(actor.GetString("name"))
		if use.ActorName == "" {
			use.ActorName, _, _ = strings.Cut(actor.GetString("email"), "@")
		}
	}
	return use, nil
}

// notionTemplateZone is the time zone a Public API request's template takes:
// the one it names, else, as Notion's API guide says, its person's for a
// public connection or a personal access token ("" here) and UTC for an
// internal connection, which is no one person's.
func notionTemplateZone(identity notionOAuthBearerIdentity, timeZone string) string {
	if timeZone == "" && notionInternalConnection(identity) {
		return "UTC"
	}
	return timeZone
}

// named is the zone a moment it writes keeps by name: its own, or UTC for the
// server's, which has no name to keep (personLocation).
func (use databaseTemplateUse) named() *time.Location {
	if use.Location == time.Local {
		return time.UTC
	}
	return use.Location
}

// validDatabaseTemplateTimeZone reports whether a request names a time zone
// template mentions can take.
func validDatabaseTemplateTimeZone(timeZone string) bool {
	if timeZone == "" {
		return true
	}
	_, err := time.LoadLocation(timeZone)
	return err == nil && timeZone != "Local" && len(timeZone) <= 64
}

// Template property values that take their value when the template is used.
const (
	databaseTemplateValueToday = "@today"
	databaseTemplateValueNow   = "@now"
	databaseTemplateValueMe    = "@me"
)

// resolveValue gives a template property value its value for this use.
func (use databaseTemplateUse) resolveValue(valueType string, raw json.RawMessage) json.RawMessage {
	local := use.At.In(use.Location)
	switch valueType {
	case graphValueTypeDate:
		var marker string
		if json.Unmarshal(raw, &marker) != nil {
			return raw
		}
		var value any
		switch marker {
		case databaseTemplateValueToday:
			value = local.Format("2006-01-02")
		case databaseTemplateValueNow:
			value = map[string]string{"start": use.At.In(use.named()).Format("2006-01-02T15:04"), "timeZone": use.named().String()}
		default:
			return raw
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return raw
		}
		return encoded
	case graphValueTypePerson:
		var people []any
		if json.Unmarshal(raw, &people) != nil {
			return raw
		}
		resolved, seen, changed := make([]any, 0, len(people)), map[string]bool{}, false
		for _, person := range people {
			if person == databaseTemplateValueMe {
				person, changed = use.ActorID, true
			}
			if id, ok := person.(string); ok {
				if seen[id] {
					continue
				}
				seen[id] = true
			}
			resolved = append(resolved, person)
		}
		if !changed {
			return raw
		}
		encoded, err := json.Marshal(resolved)
		if err != nil {
			return raw
		}
		return encoded
	}
	return raw
}

// resolve gives the body text's template mentions their value.
func (use databaseTemplateUse) resolve(spans []graphTextSpan) []graphTextSpan {
	local := use.At.In(use.Location)
	for index, span := range spans {
		if span.Mention != graphTemplateMention {
			continue
		}
		switch span.Template {
		case graphTemplateMentionToday:
			span.Mention, span.Date = "date", local.Format("2006-01-02")
			span.Text = "@" + span.Date
		case graphTemplateMentionNow:
			stamp := use.At.In(use.named())
			span.Mention, span.Date, span.DateTimeZone = "date", stamp.Format("2006-01-02T15:04"), use.named().String()
			span.Text = "@" + stamp.Format("2006-01-02 15:04")
		case graphTemplateMentionMe:
			span.Mention, span.UserID = "person", use.ActorID
			span.Text = "@" + use.ActorName
		default:
			span.Mention = ""
		}
		span.Template = ""
		spans[index] = span
	}
	return spans
}
