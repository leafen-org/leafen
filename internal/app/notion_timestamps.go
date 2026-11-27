package app

import "time"

// Notion's API writes the times of pages, blocks, data sources, page
// properties and file uploads to the minute, its seconds dropped (seen
// 2026-11-10: "2026-11-10T08:58:00.000Z"), and a database object's to the
// millisecond with its offset written out ("2026-11-10T09:00:50.367+00:00"),
// in 2025-09-03 and 2026-03-11 alike.

func notionMinuteTimestamp(at time.Time) string {
	return at.UTC().Truncate(time.Minute).Format("2006-01-02T15:04:05.000Z")
}

func notionDatabaseObjectTime(value int64) (string, error) {
	if value < 1 {
		return "", errNotionDatabaseRowPageProjectionInvalid
	}
	return time.UnixMilli(value).UTC().Format("2006-01-02T15:04:05.000+00:00"), nil
}

// notionPageMinuteTime reads a page record's stored time as the API writes it.
func notionPageMinuteTime(value string) (string, error) {
	written, err := notionPageTime(value)
	if err != nil {
		return "", err
	}
	at, err := time.Parse(time.RFC3339Nano, written)
	if err != nil {
		return "", errInvalidNotionPageIdentity
	}
	return notionMinuteTimestamp(at), nil
}
