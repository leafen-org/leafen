package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databasePropertyOwnPlace is the place a property written takes among its
// data source's: the one asked for, unless another property holds it. A
// writer places a new property from the list it has, which may not yet hold
// one made just before — in this tab, another, or by the server (a relation's
// pair) — and two shared a place (2026-11-12: a formula's copy and a relation
// made after it). A new property on a held place goes right after the
// holders, before the next, so the one made later comes later; a property
// already placed that is sent back on a held place, as a writer that hasn't
// read the move sends it with another change, keeps its own.
func databasePropertyOwnPlace(app core.App, owner string, property graphProperty) (string, error) {
	params := dbx.Params{
		"owner": owner, "source": property.DataSourceID, "position": property.Position, "id": property.ID,
	}
	var held string
	// The property is found by its source and place; the owner is read
	// after (+), or SQLite, which keeps no statistics, reads the owner's
	// every property.
	err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND +[[owner]] = {:owner} AND [[position]] = {:position} AND [[id]] <> {:id}
		LIMIT 1`).Bind(params).Row(&held)
	if errors.Is(err, sql.ErrNoRows) {
		return property.Position, nil
	}
	if err != nil {
		return "", err
	}
	var stored string
	err = app.DB().NewQuery(`SELECT [[position]] FROM {{leafen_database_properties}}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[data_source_id]] = {:source}`).Bind(params).Row(&stored)
	if err == nil && stored != property.Position {
		return stored, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	next := ""
	if err := app.DB().NewQuery(`SELECT COALESCE(MIN([[position]]), '') FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND [[owner]] = {:owner} AND [[position]] > {:position} AND [[id]] <> {:id}`).
		Bind(params).Row(&next); err != nil {
		return "", err
	}
	return databasePositionBetween(property.Position, next)
}
