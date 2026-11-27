package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// wikiSourceProperties are the properties a wiki's data source names for
// its pages' owners and verification.
type wikiSourceProperties struct {
	Owner        string `db:"wiki_owner_property_id"`
	Verification string `db:"wiki_verification_property_id"`
}

// loadWikiSourceProperties reads the Owner and Verification properties of a
// wiki's data source, and reports whether the data source is a wiki's.
func loadWikiSourceProperties(app core.App, dataSourceID string) (wikiSourceProperties, bool, error) {
	properties := wikiSourceProperties{}
	err := app.DB().NewQuery(`
		SELECT [[wiki_owner_property_id]], [[wiki_verification_property_id]] FROM {{leafen_data_sources}}
		WHERE [[id]] = {:id} AND [[wiki]] = 1
	`).Bind(dbx.Params{"id": dataSourceID}).One(&properties)
	if errors.Is(err, sql.ErrNoRows) {
		return wikiSourceProperties{}, false, nil
	}
	return properties, err == nil, err
}

// verificationSourceProperties are the Owner and Verification properties a
// data source names: a wiki's, or any database's given a Verification
// property (database_verification_properties.go).
type verificationSourceProperties struct {
	wikiSourceProperties
	Wiki bool `db:"wiki"`
}

// loadVerificationSourceProperties reads the Owner and Verification
// properties a data source names, and reports whether it names a
// Verification property.
func loadVerificationSourceProperties(app core.App, dataSourceID string) (verificationSourceProperties, bool, error) {
	properties := verificationSourceProperties{}
	err := app.DB().NewQuery(`
		SELECT [[wiki_owner_property_id]], [[wiki_verification_property_id]], [[wiki]] FROM {{leafen_data_sources}}
		WHERE [[id]] = {:id} AND ([[wiki]] = 1 OR [[wiki_verification_property_id]] != '')
	`).Bind(dbx.Params{"id": dataSourceID}).One(&properties)
	if errors.Is(err, sql.ErrNoRows) {
		return verificationSourceProperties{}, false, nil
	}
	return properties, err == nil, err
}

// prepareWikiRowValues holds pages to Notion's rules for their owners and
// verification. A wiki's page new to the wiki is owned by whoever makes it
// or moves it in unless the change names its owners, and only someone who owns a wiki's
// page verifies it or takes the verification away. Another database's page
// is verified by whoever can change it, and a page verified without owners
// is owned by whoever verifies it (Notion: a verified page has an owner); an
// Owner that defaults to the creator reads as the page's creator. A
// verification names who verified it and from when. A page new in the
// change keeps the verification it is given.
func prepareWikiRowValues(app core.App, principalID string, operations []graphOperation) (int, error) {
	sources := map[string]*verificationSourceProperties{}
	ownersFilled := 0
	now := time.Now().UTC()
	changed := false
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		row := operation.Row
		source, known := sources[row.DataSourceID]
		if !known {
			properties, named, err := loadVerificationSourceProperties(app, row.DataSourceID)
			if err != nil {
				return ownersFilled, err
			}
			if named {
				source = &properties
			}
			sources[row.DataSourceID] = source
		}
		if source == nil {
			continue
		}
		if _, named := row.Values[source.Owner]; !named && source.Owner != "" {
			// New to the wiki: made in it, or come from another database
			// (seen 2026-11-19: Notion's API owns a database page moved into
			// a wiki's data source by whoever moved it).
			var existing int
			if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_rows}} WHERE [[id]] = {:rowId} AND [[data_source_id]] = {:source}`).
				Bind(dbx.Params{"rowId": row.ID, "source": row.DataSourceID}).Row(&existing); err != nil {
				return ownersFilled, err
			}
			if existing == 0 {
				owner, err := json.Marshal([]string{principalID})
				if err != nil {
					return ownersFilled, err
				}
				if row.Values == nil {
					row.Values = map[string]graphRowValue{}
				}
				row.Values[source.Owner] = graphRowValue{Type: graphValueTypePerson, Value: owner}
				ownersFilled++
			}
		}
		value, present := row.Values[source.Verification]
		if !present {
			continue
		}
		stored := struct {
			Verification sql.NullString `db:"verification"`
			Owners       sql.NullString `db:"owners"`
			OwnerConfig  sql.NullString `db:"owner_config"`
			Creator      sql.NullString `db:"creator"`
		}{}
		err := app.DB().NewQuery(`
			SELECT
				(SELECT [[value]] FROM {{leafen_database_row_values}}
					WHERE [[row_id]] = [[row]].[[id]] AND [[property_id]] = {:verification}) AS [[verification]],
				(SELECT [[value]] FROM {{leafen_database_row_values}}
					WHERE [[row_id]] = [[row]].[[id]] AND [[property_id]] = {:owner}) AS [[owners]],
				(SELECT [[config]] FROM {{leafen_database_properties}} WHERE [[id]] = {:owner}) AS [[owner_config]],
				(SELECT [[created_by]] FROM {{leafen_pages}} WHERE [[id]] = [[row]].[[id]]) AS [[creator]]
			FROM {{leafen_database_rows}} AS [[row]] WHERE [[row]].[[id]] = {:rowId}
		`).Bind(dbx.Params{
			"rowId": row.ID, "verification": source.Verification, "owner": source.Owner,
		}).One(&stored)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return ownersFilled, err
		}
		if databaseRowNotificationSameValue(stored.Verification.String, string(value.Value)) {
			continue
		}
		owners := stored.Owners.String
		if ownerValue, ownersChange := row.Values[source.Owner]; ownersChange {
			owners = string(ownerValue.Value)
		}
		ownerIDs := []string{}
		if strings.TrimSpace(owners) != "" && json.Unmarshal([]byte(owners), &ownerIDs) != nil {
			return ownersFilled, graphInputError(errorCodeGraphInvalid, "Check the page's owners.")
		}
		// The owners as they read: an Owner that defaults to the creator is the
		// page's creator while it names no one (databaseValueAsRead).
		if len(ownerIDs) == 0 && stored.Creator.String != "" &&
			databasePersonDefaultsToCreator(graphValueTypePerson, stored.OwnerConfig.String) {
			ownerIDs = []string{stored.Creator.String}
		}
		if source.Wiki && !slices.Contains(ownerIDs, principalID) {
			return ownersFilled, graphInputError(errorCodeGraphInvalid, "Only the page's owners can verify it.")
		}
		if !source.Wiki && len(ownerIDs) == 0 && source.Owner != "" && !graphRowValueIsCleared(value.Value) {
			owner, err := json.Marshal([]string{principalID})
			if err != nil {
				return ownersFilled, err
			}
			row.Values[source.Owner] = graphRowValue{Type: graphValueTypePerson, Value: owner}
		}
		if !graphRowValueIsCleared(value.Value) {
			canonical, err := canonicalWikiVerificationValue(value.Value, principalID, now)
			if err != nil {
				return ownersFilled, err
			}
			row.Values[source.Verification] = graphRowValue{Type: graphValueTypeVerification, Value: canonical}
		}
		changed = true
	}
	if changed {
		notifyWikiVerificationAfterCommit(app)
	}
	return ownersFilled, nil
}

// canonicalWikiVerificationValue reads a verification as its owner gives
// it: verified until a later time or indefinitely, or unverified. The
// verification names the owner as its verifier and starts now unless it
// says when it starts; an expired verification is read, not written.
func canonicalWikiVerificationValue(raw json.RawMessage, principalID string, now time.Time) (json.RawMessage, error) {
	invalid := graphInputError(errorCodeGraphInvalid, "Check the verification.")
	given := struct {
		State string `json:"state"`
		Date  *struct {
			Start string  `json:"start"`
			End   *string `json:"end"`
		} `json:"date"`
	}{}
	if json.Unmarshal(raw, &given) != nil {
		return nil, invalid
	}
	switch given.State {
	case "unverified":
		return json.RawMessage(`{"state":"unverified"}`), nil
	case "verified":
	default:
		return nil, invalid
	}
	start := now
	var end any
	if given.Date != nil {
		if strings.TrimSpace(given.Date.Start) != "" {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(given.Date.Start))
			if err != nil {
				return nil, invalid
			}
			start = parsed.UTC()
		}
		if given.Date.End != nil {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*given.Date.End))
			if err != nil || !parsed.After(now) || !parsed.After(start) {
				return nil, invalid
			}
			end = parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return json.Marshal(map[string]any{
		"state": "verified", "verified_by": principalID,
		"date": map[string]any{"start": start.Format(time.RFC3339Nano), "end": end, "time_zone": nil},
	})
}
