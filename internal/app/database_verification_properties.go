package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// storedVerificationSource is what a data source names for its pages'
// verification (schema_baseline.json).
type storedVerificationSource struct {
	ID           string `db:"id"`
	Wiki         bool   `db:"wiki"`
	Verification string `db:"wiki_verification_property_id"`
	Owner        string `db:"wiki_owner_property_id"`
}

// propertiesTrashedKey carries the properties a change sends to Deleted
// properties once it is applied, apart from its operations, as the
// Notion-compatible API and MCP remove one (withPropertiesTrashed).
type propertiesTrashedKey struct{}

// withPropertiesTrashed names the properties the change planned in ctx
// sends to Deleted properties, which its planning fills in: a Verification
// or Owner among them names nothing for the change's new Verification.
func withPropertiesTrashed(ctx context.Context, trashed *[]string) context.Context {
	return context.WithValue(ctx, propertiesTrashedKey{}, trashed)
}

// propertiesTrashedBy is the properties the change planned in ctx sends to
// Deleted properties once it is applied (withPropertiesTrashed).
func propertiesTrashedBy(ctx context.Context) map[string]bool {
	ids := map[string]bool{}
	if ctx == nil {
		return ids
	}
	if trashed, ok := ctx.Value(propertiesTrashedKey{}).(*[]string); ok && trashed != nil {
		for _, id := range *trashed {
			ids[id] = true
		}
	}
	return ids
}

type verificationSourceLink struct {
	source, verification, owner string
	// backfill: the source was there before the change, so its pages
	// verified one by one carry their verification into the new values.
	backfill bool
}

// planDatabaseVerificationProperties gives a database's new Verification
// property what Notion's comes with: the data source names it and an Owner
// property — the one it names already, or a new one in the language of the
// person adding it — so its pages' verification follows the values. A page
// verified on its own before keeps its verification and owners, now as the
// values. Removing the named Verification or Owner property, or making it
// another type, unnames it. A wiki keeps the two it was made with, and a
// database naming a Verification property keeps that one; another is a
// plain property — the second of two a change makes too, so a data source
// has one Owner (2026-11-12: two made at once took "Owner" and "Owner 1").
// A Verification or Owner the same change removes or retypes names nothing.
// The returned step runs once the change is applied.
func planDatabaseVerificationProperties(
	ctx context.Context, app core.App, principalID string, operations []graphOperation,
) ([]graphOperation, func(core.App) error, error) {
	sources := map[string]*storedVerificationSource{}
	loadSource := func(id string) (*storedVerificationSource, error) {
		if source, known := sources[id]; known {
			return source, nil
		}
		source := &storedVerificationSource{}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[wiki]], [[wiki_verification_property_id]], [[wiki_owner_property_id]]
			FROM {{leafen_data_sources}} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": id}).One(source)
		if errors.Is(err, sql.ErrNoRows) {
			source = nil
		} else if err != nil {
			return nil, err
		}
		sources[id] = source
		return source, nil
	}
	// A property in the trash names nothing: a Verification added after its
	// named one was deleted takes an Owner of its own (2026-11-04: the new one
	// stayed unnamed, its Owner never made).
	propertyType := func(id string) (string, error) {
		var current string
		err := app.DB().NewQuery(`
			SELECT [[type]] FROM {{leafen_database_properties}}
			WHERE [[id]] = {:id} AND NOT EXISTS (SELECT 1 FROM {{leafen_database_property_trash}} WHERE [[property_id]] = {:id})
		`).Bind(dbx.Params{"id": id}).Row(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return current, err
	}

	// unnamed is each property the change removes or makes another type,
	// which names nothing once it is applied; verifications is each one it
	// makes a Verification.
	unnamed := []string{}
	gone := propertiesTrashedBy(ctx)
	verifications := []*graphProperty{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil:
			unnamed = append(unnamed, operation.PropertyDeletion.ID)
			gone[operation.PropertyDeletion.ID] = true
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			property := operation.Property
			current, err := propertyType(property.ID)
			if err != nil {
				return nil, nil, err
			}
			if current != "" && current != property.Type {
				unnamed = append(unnamed, property.ID)
				gone[property.ID] = true
			}
			if property.Type == graphValueTypeVerification && current != graphValueTypeVerification {
				verifications = append(verifications, property)
			}
		}
	}
	// named is a property the data source names that stays the type it
	// names it as.
	named := func(id, wanted string) (bool, error) {
		if id == "" || gone[id] {
			return false, nil
		}
		current, err := propertyType(id)
		return current == wanted, err
	}
	links := []verificationSourceLink{}
	linked := map[string]bool{}
	added := []graphOperation{}
	for _, property := range verifications {
		if linked[property.DataSourceID] {
			continue
		}
		source, err := loadSource(property.DataSourceID)
		if err != nil {
			return nil, nil, err
		}
		if source != nil && source.Wiki {
			continue
		}
		if source != nil && source.Verification != property.ID {
			if kept, err := named(source.Verification, graphValueTypeVerification); err != nil {
				return nil, nil, err
			} else if kept {
				continue
			}
		}
		linked[property.DataSourceID] = true
		link := verificationSourceLink{source: property.DataSourceID, verification: property.ID, backfill: source != nil}
		if source != nil {
			if kept, err := named(source.Owner, graphValueTypePerson); err != nil {
				return nil, nil, err
			} else if kept {
				link.owner = source.Owner
			}
		}
		// A new Owner reads as each page's creator until it names someone, as
		// Notion's Owner comes with "기본값: 생성자" and shows the creator in
		// every row at once (seen 2026-11-11).
		if link.owner == "" {
			link.owner = uuid.NewString()
			added = append(added, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
				ID: link.owner, DataSourceID: property.DataSourceID, Type: graphValueTypePerson,
				Name: generatedNamesFor(ctx, app, principalID).Wiki.Owner, Position: property.Position + "耀",
				Config: json.RawMessage(`{"defaultCreator":true}`),
			}})
		}
		// The Verification property says which is its Owner, so the app fills
		// the owner as the server does, at once.
		config := map[string]any{}
		if len(property.Config) > 0 && json.Unmarshal(property.Config, &config) != nil {
			config = map[string]any{}
		}
		config["ownerPropertyId"] = link.owner
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, nil, err
		}
		property.Config = encoded
		links = append(links, link)
	}
	if len(links) == 0 && len(unnamed) == 0 {
		return operations, nil, nil
	}
	showVerificationOwners(operations, links)
	operations = append(operations, added...)
	return operations, func(txApp core.App) error {
		for _, id := range unnamed {
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_data_sources}} SET [[wiki_verification_property_id]] = ''
				WHERE [[wiki]] = 0 AND [[wiki_verification_property_id]] = {:id};
				UPDATE {{leafen_data_sources}} SET [[wiki_owner_property_id]] = ''
				WHERE [[wiki]] = 0 AND [[wiki_owner_property_id]] = {:id};
			`).Bind(dbx.Params{"id": id}).Execute(); err != nil {
				return err
			}
		}
		for _, link := range links {
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_data_sources}}
				SET [[wiki_verification_property_id]] = {:verification}, [[wiki_owner_property_id]] = {:owner}
				WHERE [[id]] = {:source} AND [[wiki]] = 0
			`).Bind(dbx.Params{"source": link.source, "verification": link.verification, "owner": link.owner}).Execute(); err != nil {
				return err
			}
			if link.backfill {
				if err := carryPageVerificationIntoValues(txApp, link); err != nil {
					return err
				}
			}
		}
		return nil
	}, nil
}

// verifiedDatabasePage is a database's page verified, or given owners, one
// by one.
type verifiedDatabasePage struct {
	RowID       string `db:"row_id"`
	Owner       string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	VerifiedAt  string `db:"verified_at"`
	VerifiedBy  string `db:"verified_by"`
	ExpiresAt   string `db:"verification_expires_at"`
}

// carryPageVerificationIntoValues writes the verification and owners of a
// database's pages verified one by one as its new Verification and Owner
// values, which the page then follows, and drops the page's own owners.
func carryPageVerificationIntoValues(app core.App, link verificationSourceLink) error {
	pages := []verifiedDatabasePage{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]] AS [[row_id]], [[row]].[[owner]], [[row]].[[workspace_id]], [[row]].[[page_id]],
			[[page]].[[verified_at]], [[page]].[[verified_by]], [[page]].[[verification_expires_at]]
		FROM {{leafen_database_rows}} AS [[row]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[row]].[[id]]
		WHERE ` + databaseSourceRowsSQL("[[row]]", "{:source}") + ` AND ([[page]].[[verified_at]] != '' OR EXISTS (
			SELECT 1 FROM {{leafen_wiki_page_owners}} AS [[owners]]
			WHERE [[owners]].[[page_id]] = [[row]].[[id]] AND [[owners]].[[data_source_id]] = ''))
	`).Bind(dbx.Params{"source": link.source}).All(&pages); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	write := func(page verifiedDatabasePage, propertyID, valueType string, value []byte) error {
		_, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_row_values}}
				([[owner]], [[workspace_id]], [[page_id]], [[row_id]], [[property_id]], [[type]], [[value]], [[created]], [[updated]])
			VALUES ({:owner}, {:workspace}, {:page}, {:row}, {:property}, {:type}, {:value}, {:now}, {:now})
			ON CONFLICT ([[row_id]], [[property_id]]) DO UPDATE SET [[type]] = excluded.[[type]], [[value]] = excluded.[[value]],
				[[updated]] = excluded.[[updated]]
		`).Bind(dbx.Params{
			"owner": page.Owner, "workspace": page.WorkspaceID, "page": page.PageID, "row": page.RowID,
			"property": propertyID, "type": valueType, "value": string(value), "now": now,
		}).Execute()
		return err
	}
	for _, page := range pages {
		owners := []string{}
		if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_wiki_page_owners}}
			WHERE [[page_id]] = {:page} AND [[data_source_id]] = '' ORDER BY [[position]], [[user_id]]`).
			Bind(dbx.Params{"page": page.RowID}).Column(&owners); err != nil {
			return err
		}
		if len(owners) > 0 {
			value, err := json.Marshal(owners)
			if err != nil {
				return err
			}
			if err := write(page, link.owner, graphValueTypePerson, value); err != nil {
				return err
			}
		}
		if page.VerifiedAt != "" {
			var end any
			if page.ExpiresAt != "" {
				end = page.ExpiresAt
			}
			value, err := json.Marshal(map[string]any{
				"state": "verified", "verified_by": page.VerifiedBy,
				"date": map[string]any{"start": page.VerifiedAt, "end": end, "time_zone": nil},
			})
			if err != nil {
				return err
			}
			if err := write(page, link.verification, graphValueTypeVerification, value); err != nil {
				return err
			}
		}
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_wiki_page_owners}}
			WHERE [[page_id]] = {:page} AND [[data_source_id]] = ''`).Bind(dbx.Params{"page": page.RowID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// showVerificationOwners shows a new Verification's Owner right after it in
// the views the same change makes show the Verification, as Notion's table
// puts its Owner column there (seen 2026-11-03): a view that lists the
// properties it shows would otherwise hide the Owner the server added. A view
// the change makes hide the Verification — another table of the database
// that set what it shows (database_new_property_hiding.go) — hides its Owner
// with it.
func showVerificationOwners(operations []graphOperation, links []verificationSourceLink) {
	for index, operation := range operations {
		if operation.Kind != graphOperationKindUpsertView || operation.View == nil {
			continue
		}
		config := map[string]any{}
		if json.Unmarshal(operation.View.Config, &config) != nil {
			continue
		}
		changed := false
		for _, link := range links {
			for _, key := range []string{"visibleProperties", "propertyOrder"} {
				listed, ok := config[key].([]any)
				if !ok {
					continue
				}
				at, owned := -1, false
				for position, id := range listed {
					if id == link.verification {
						at = position
					}
					if id == link.owner {
						owned = true
					}
				}
				if at < 0 || owned {
					continue
				}
				next := make([]any, 0, len(listed)+1)
				next = append(next, listed[:at+1]...)
				next = append(next, link.owner)
				config[key] = append(next, listed[at+1:]...)
				changed = true
			}
			if hidden, ok := config["hiddenProperties"].([]any); ok && slices.Contains(hidden, any(link.verification)) &&
				!slices.Contains(hidden, any(link.owner)) {
				config["hiddenProperties"], changed = append(hidden, link.owner), true
			}
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			continue
		}
		view := *operation.View
		view.Config = encoded
		operations[index].View = &view
	}
}
