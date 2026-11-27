package app

import (
	"unicode/utf8"
)

const notificationEventsTable = "leafen_notification_events"

// notificationEventTitle is the name an event keeps of its page: as much of
// the title as its 8 KiB holds, whole characters, since a title runs up to
// maxGraphTitleRunes.
func notificationEventTitle(title string) string {
	const maxBytes = 8192
	if len(title) <= maxBytes {
		return title
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(title[cut]) {
		cut--
	}
	return title[:cut]
}
