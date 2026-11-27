package app

import (
	"bytes"
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A public page's mention of a database without a name of several sources
// — a page mention of a full-page one's page or a database mention, as the
// "@" menu makes one — carries their titles, as its visitor reads them, for
// the app to name it by them, "A 및 B", as a member's names it
// (database_container_name.go), one it shows from another database marked
// (database_source_titles.go).
const publicMentionSourceTitlesKey = "mentionSourceTitles"

// publicMentionedDatabasesAsRead puts the names of the databases a public
// page mentions as its visitor reads them (databaseContainerNamesAsRead),
// and says, by each mention's database, the sources' titles that name one
// without a name of several, its name then left empty.
func publicMentionedDatabasesAsRead(
	app core.App, closer func() searchMentionCloser, databases map[string]storedPublicMentionedDatabase,
) (map[string][]databaseSourceTitle, error) {
	ids := make([]string, 0, len(databases))
	for target := range databases {
		ids = append(ids, target)
	}
	titles, named, err := databaseContainerNamesAsRead(app, closer, ids)
	if err != nil {
		return nil, err
	}
	sources := map[string][]databaseSourceTitle{}
	for target, database := range databases {
		database.Title = oneMarkPerMention(titles[target])
		databases[target] = database
		for _, title := range named[target] {
			sources[target] = append(sources[target], databaseSourceTitle{Title: oneMarkPerMention(title.Title), Linked: title.Linked})
		}
	}
	return sources, nil
}

// publicSpanValueEmpty reports whether a value a span is given is empty or
// no, which the span then drops (publicSpanWith).
func publicSpanValueEmpty(value any) bool {
	switch value := value.(type) {
	case string:
		return value == ""
	case bool:
		return !value
	case []string:
		return len(value) == 0
	case []databaseSourceTitle:
		return len(value) == 0
	}
	return false
}

// publicSpanWordsAre reports whether a span's value is the words given,
// whether a stored span holds them as words or as a list of anything.
func publicSpanWordsAre(current, value any) bool {
	if titles, listed := value.([]databaseSourceTitle); listed {
		// Sources' titles as the wire writes them, whichever way they are held.
		written, err := json.Marshal(titles)
		held, heldErr := json.Marshal(current)
		return err == nil && heldErr == nil && bytes.Equal(written, held)
	}
	words, ok := value.([]string)
	if !ok {
		return false
	}
	switch current := current.(type) {
	case []string:
		if len(current) != len(words) {
			return false
		}
		for index := range words {
			if current[index] != words[index] {
				return false
			}
		}
		return true
	case []any:
		if len(current) != len(words) {
			return false
		}
		for index := range words {
			if text, isText := current[index].(string); !isText || text != words[index] {
				return false
			}
		}
		return true
	}
	return false
}
