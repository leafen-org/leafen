package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// A list read a window at a time goes on from the time and id of the last
// item it gave (or looked at, when it filters what it reads); the cursor is
// that pair, opaque to clients.

type timeIDCursor struct {
	At int64  `json:"at"`
	ID string `json:"id"`
}

var errListWindowCursor = errors.New("the cursor isn't one this list gave")

func encodeTimeIDCursor(at int64, id string) string {
	encoded, _ := json.Marshal(timeIDCursor{At: at, ID: id})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// timeIDWindowOf keeps the first size of items read one past it, and the
// cursor of the last one kept when more follow.
func timeIDWindowOf[T any](items []T, size int, cursor func(T) (int64, string)) ([]T, string) {
	if len(items) <= size {
		return items, ""
	}
	items = items[:size]
	at, id := cursor(items[size-1])
	return items, encodeTimeIDCursor(at, id)
}

// decodeTimeIDCursor reads a cursor; "" is the start, with an empty ID.
func decodeTimeIDCursor(raw string) (timeIDCursor, error) {
	cursor := timeIDCursor{}
	if raw = strings.TrimSpace(raw); raw == "" {
		return cursor, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.ID == "" {
		return timeIDCursor{}, errListWindowCursor
	}
	return cursor, nil
}

// textIDCursor is the same for a list ordered by a text time (RFC 3339).
type textIDCursor struct {
	At string `json:"at"`
	ID string `json:"id"`
}

func encodeTextIDCursor(at, id string) string {
	encoded, _ := json.Marshal(textIDCursor{At: at, ID: id})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeTextIDCursor(raw string) (textIDCursor, error) {
	cursor := textIDCursor{}
	if raw = strings.TrimSpace(raw); raw == "" {
		return cursor, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.ID == "" {
		return textIDCursor{}, errListWindowCursor
	}
	return cursor, nil
}
