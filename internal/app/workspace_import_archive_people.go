package app

import (
	"database/sql"
	"errors"
	"strings"
)

// A workspace moved here from another Leafen server comes with its people
// and the accounts here each was paired with (the workspace move,
// workspace_move*.go), beside a bundle whose people are the old server's
// IDs. A mention, a person value, a comment's author and its reactions, and
// who made and last changed a page then name the account paired. A person
// not paired becomes a stand-in, `leafen-user:<their ID>` with their name and
// email, which Settings › People › Imported people pairs once they have an
// account here, as it pairs Notion's people (imported_people_mapping_*.go). An import that isn't
// a move, or a person the move didn't name, keeps the ID the bundle gave.

const workspaceImportPersonPrefix = "leafen-user:"

// workspaceImportPerson is one person of a moved workspace: their ID on the
// old server, the account here paired with them, if any, and what they are
// called.
type workspaceImportPerson struct {
	SourceID string `json:"sourceId"`
	TargetID string `json:"targetId,omitempty"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	// PairOf is, for a person's pair bot, the person: the bot is the pair bot
	// here of the account the person was paired with.
	PairOf string `json:"pairOf,omitempty"`
}

const workspaceImportArchivePeopleTable = `
	CREATE TABLE people (
		source_id TEXT PRIMARY KEY,
		target_id TEXT NOT NULL,
		name TEXT NOT NULL,
		email TEXT NOT NULL
	);
`

// fillWorkspaceImportArchivePeople keeps a move's people in the import's map.
func fillWorkspaceImportArchivePeople(db *sql.DB, people []workspaceImportPerson) error {
	for _, person := range people {
		if strings.TrimSpace(person.SourceID) == "" {
			return workspaceImportInputError()
		}
		if _, err := db.Exec(`INSERT OR REPLACE INTO people (source_id, target_id, name, email) VALUES (?, ?, ?, ?)`,
			person.SourceID, person.TargetID, strings.TrimSpace(person.Name), strings.ToLower(strings.TrimSpace(person.Email))); err != nil {
			return workspaceImportStorageError(err)
		}
	}
	return nil
}

// workspaceImportArchivePerson is who a person the bundle names is here: the
// account paired, or the stand-in's ID with the name and email it carries;
// false for one the move didn't name.
func workspaceImportArchivePerson(db workspaceImportArchiveQueryer, sourceID string) (string, map[string]any, bool, error) {
	if sourceID == "" {
		return "", nil, false, nil
	}
	var target, name, email string
	err := db.QueryRow(`SELECT target_id, name, email FROM people WHERE source_id = ?`, sourceID).Scan(&target, &name, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return sourceID, nil, false, nil
	}
	if err != nil {
		return "", nil, false, workspaceImportStorageError(err)
	}
	if target != "" {
		return target, nil, true, nil
	}
	standIn := map[string]any{"displayName": nil, "email": nil}
	if name != "" {
		standIn["displayName"] = name
	}
	if email != "" {
		standIn["email"] = email
	}
	return workspaceImportPersonPrefix + sourceID, standIn, true, nil
}

// workspaceImportArchiveMention makes a mention's person the one here: its
// `userId`, and for a stand-in its name and email beside it.
func workspaceImportArchiveMention(db workspaceImportArchiveQueryer, attributes map[string]any) error {
	sourceID, _ := attributes["userId"].(string)
	target, standIn, known, err := workspaceImportArchivePerson(db, sourceID)
	if err != nil || !known {
		return err
	}
	attributes["userId"] = target
	if standIn != nil {
		attributes["leafenUser"] = standIn
	}
	return nil
}

// workspaceImportArchivePersonValue makes a person value's people the ones
// here; a stand-in is kept as an object, as an imported Notion person is.
func workspaceImportArchivePersonValue(db workspaceImportArchiveQueryer, value any) (any, error) {
	people, ok := value.([]any)
	if !ok {
		return value, nil
	}
	for index, item := range people {
		sourceID, _ := item.(string)
		target, standIn, known, err := workspaceImportArchivePerson(db, sourceID)
		if err != nil {
			return nil, err
		}
		if !known {
			continue
		}
		if standIn == nil {
			people[index] = target
			continue
		}
		standIn["id"], standIn["userId"] = target, target
		people[index] = standIn
	}
	return people, nil
}

// workspaceImportArchiveAccount is the account here a person the bundle names
// stands for, or "" for none: a stand-in has no account.
func workspaceImportArchiveAccount(db workspaceImportArchiveQueryer, sourceID string) (string, error) {
	target, standIn, known, err := workspaceImportArchivePerson(db, sourceID)
	if err != nil || !known || standIn != nil {
		return "", err
	}
	return target, nil
}
