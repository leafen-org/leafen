package app

import (
	"encoding/json"
	"strings"
)

// An untitled database of several sources goes by its sources' names
// listed, and among them a source it shows from another database by that
// source's linked view's name, "〈원본〉 보기", as Notion's names it on the
// screen (seen 2026-11-25 in Chrome at 1440, twice at different times: an
// inline database of one source of its own and one it links opened alone
// titled "C 및 X 보기" in its placeholder, its window and path the same and a
// mention of it the same; one of two of its own and one linked "A, B, X 보기";
// the data source picker's path to its own source "… / C 및 X 보기"). Its API
// names it by its own sources alone and so lists none (databaseNamingAPI);
// its MCP fetch heads it as the screen does. Every list
// carrying those names carries this mark with each (databaseSourceTitle), for
// the app to name it in its reader's language (untitledDatabaseSourcesName,
// databaseTitles.ts), and the server so in a message it writes
// (untitledDatabaseSourcesNameIn).

// databaseSourceTitle is one of the names an untitled database of several
// sources is listed by: a source's title, "" for an untitled one, and whether
// the database shows it from another (Linked). On the wire one of its own is
// its title alone and a linked one {"title": …, "linked": true}, so a list
// kept from before reads as it did.
type databaseSourceTitle struct {
	Title  string
	Linked bool
}

// databaseLinkedSourceTitleWire is a linked one's mark, its keys in the order
// a stored list read back writes them.
type databaseLinkedSourceTitleWire struct {
	Linked bool   `json:"linked"`
	Title  string `json:"title"`
}

// MarshalJSON writes a source of its own by its title, a linked one marked.
func (title databaseSourceTitle) MarshalJSON() ([]byte, error) {
	if !title.Linked {
		return json.Marshal(title.Title)
	}
	return json.Marshal(databaseLinkedSourceTitleWire{Linked: true, Title: title.Title})
}

// UnmarshalJSON reads either.
func (title *databaseSourceTitle) UnmarshalJSON(data []byte) error {
	if strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		linked := databaseLinkedSourceTitleWire{}
		if err := json.Unmarshal(data, &linked); err != nil {
			return err
		}
		*title = databaseSourceTitle{Title: linked.Title, Linked: linked.Linked}
		return nil
	}
	*title = databaseSourceTitle{}
	return json.Unmarshal(data, &title.Title)
}

// databaseSourceTitlesOf are the names a database's naming sources list it
// by, as read.
func databaseSourceTitlesOf(sources []databaseNameRead) []databaseSourceTitle {
	if len(sources) == 0 {
		return nil
	}
	titles := make([]databaseSourceTitle, len(sources))
	for index, source := range sources {
		titles[index] = databaseSourceTitle{Title: source.Title, Linked: source.Linked}
	}
	return titles
}

// ownDatabaseSourceTitles are titles of a database's own sources, none
// linked: names read from Notion, which say none.
func ownDatabaseSourceTitles(titles []string) []databaseSourceTitle {
	listed := make([]databaseSourceTitle, len(titles))
	for index, title := range titles {
		listed[index] = databaseSourceTitle{Title: title}
	}
	return listed
}
