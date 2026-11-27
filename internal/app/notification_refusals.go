package app

import "strings"

// What a refused notification list, read, archive or state change says to
// fix (request_refusal.go): the field and why after the route's words.
// Status and code stay the route's.

// notificationIDsRefusal is why a list of notification ids can't be taken,
// or nil.
func notificationIDsRefusal(kind error, ids []string, most int) error {
	if len(ids) < 1 || len(ids) > most {
		return refuse(kind, "notificationIds names 1 to %d notifications, not %d", most, len(ids))
	}
	for index, id := range ids {
		if !validNotificationReadID(strings.TrimSpace(id)) {
			return refuse(kind, "notificationIds[%d] isn't a notification's id", index)
		}
	}
	return nil
}

// notificationRequestIDRefusal is a request id that isn't one.
func notificationRequestIDRefusal(kind error) error {
	return refuse(kind, "requestId is a new id of yours, 1–%d characters, so the same change sent again isn't made twice", commentIdentifierRunes)
}
