package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A new wiki's database takes the names its maker's request sends, in the
// web's language, or else the names the server gives it in its maker's
// language. A conversion keeps the names in its job until it makes the
// database; a job without them names the database in English.

// wikiDatabaseJobPayload is what a wiki conversion keeps for its database
// until it completes.
type wikiDatabaseJobPayload struct {
	Names *wikiDatabaseNames `json:"names,omitempty"`
}

// completeWikiDatabaseNames trims the names a request sends and takes the
// default for any it leaves empty; a name longer than a property's or view's
// name may be is refused.
func completeWikiDatabaseNames(names *wikiDatabaseNames, defaults wikiDatabaseNames) (wikiDatabaseNames, error) {
	complete := defaults
	if names == nil {
		return complete, nil
	}
	for _, field := range []struct {
		name, value string
		complete    *string
	}{
		{"page", names.Page, &complete.Page}, {"owner", names.Owner, &complete.Owner}, {"tags", names.Tags, &complete.Tags},
		{"verification", names.Verification, &complete.Verification}, {"lastEditedTime", names.LastEditedTime, &complete.LastEditedTime},
		{"allPages", names.AllPages, &complete.AllPages}, {"pagesIOwn", names.PagesIOwn, &complete.PagesIOwn},
		{"parentItem", names.ParentItem, &complete.ParentItem}, {"subItems", names.SubItems, &complete.SubItems},
	} {
		value := strings.TrimSpace(field.value)
		if count := len([]rune(value)); count > maxGraphIdentifierLength {
			return wikiDatabaseNames{}, wikiRefused("names.%s is %d characters, over %d", field.name, count, maxGraphIdentifierLength)
		}
		if value != "" {
			*field.complete = value
		}
	}
	return complete, nil
}

// wikiDatabaseJobPayloadJSON is the payload a conversion job starts with.
func wikiDatabaseJobPayloadJSON(names wikiDatabaseNames) (string, error) {
	encoded, err := json.Marshal(wikiDatabaseJobPayload{Names: &names})
	return string(encoded), err
}

// loadWikiDatabaseNames reads the names a conversion job keeps for its
// database.
func loadWikiDatabaseNames(app core.App, jobID string) (wikiDatabaseNames, error) {
	english := generatedNamesIn(accountLanguageEn).Wiki
	payload := ""
	err := app.DB().NewQuery(`
		SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]] = {:jobId} AND [[kind]] = {:kind}
	`).Bind(dbx.Params{"jobId": jobID, "kind": wikiMutationJobKind}).Row(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return english, nil
	}
	if err != nil {
		return wikiDatabaseNames{}, err
	}
	decoded := wikiDatabaseJobPayload{}
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return english, nil
	}
	return completeWikiDatabaseNames(decoded.Names, english)
}
