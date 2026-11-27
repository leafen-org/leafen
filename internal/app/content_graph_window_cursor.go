package app

import (
	"errors"
	"strconv"
	"strings"
)

// A cursor names its part and the last item's keys, each kept whole with its
// length before it: a place or an id may hold any character, so keys joined
// by a separator neither split back nor sorted as their columns do.

const graphWindowCursorVersion = "2|"

// The parts of a window, in order, and how many keys each item has.
const (
	graphWindowBlocks      = "01"
	graphWindowContainers  = "02"
	graphWindowDataSources = "03"
	graphWindowViews       = "04"
	graphWindowProperties  = "05"
	graphWindowRows        = "06"
	graphWindowDocument    = "07"
)

var graphWindowKeyCounts = map[string]int{
	graphWindowBlocks: 3, graphWindowContainers: 2, graphWindowDataSources: 1,
	graphWindowViews: 2, graphWindowProperties: 2, graphWindowRows: 3, graphWindowDocument: 1,
}

type graphWindowCursor struct {
	part string
	keys []string
}

func encodeGraphWindowCursor(part string, keys []string) string {
	var cursor strings.Builder
	cursor.WriteString(graphWindowCursorVersion)
	cursor.WriteString(part)
	for _, key := range keys {
		cursor.WriteByte('|')
		cursor.WriteString(strconv.Itoa(len(key)))
		cursor.WriteByte(':')
		cursor.WriteString(key)
	}
	return cursor.String()
}

func parseGraphWindowCursor(raw string) (graphWindowCursor, error) {
	if raw == "" {
		return graphWindowCursor{}, nil
	}
	invalid := errors.New(errorCodeGraphCursorInvalid)
	rest, ok := strings.CutPrefix(raw, graphWindowCursorVersion)
	if !ok || len(rest) < 2 {
		return graphWindowCursor{}, invalid
	}
	cursor := graphWindowCursor{part: rest[:2]}
	count, known := graphWindowKeyCounts[cursor.part]
	if !known {
		return graphWindowCursor{}, invalid
	}
	rest = rest[2:]
	for rest != "" {
		colon := strings.IndexByte(rest, ':')
		if rest[0] != '|' || colon < 2 {
			return graphWindowCursor{}, invalid
		}
		length, err := strconv.Atoi(rest[1:colon])
		if err != nil || length < 0 || colon+1+length > len(rest) {
			return graphWindowCursor{}, invalid
		}
		cursor.keys = append(cursor.keys, rest[colon+1:colon+1+length])
		rest = rest[colon+1+length:]
	}
	if len(cursor.keys) != count {
		return graphWindowCursor{}, invalid
	}
	return cursor, nil
}
