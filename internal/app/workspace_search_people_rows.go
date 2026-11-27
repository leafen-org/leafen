package app

// ofRow is the location of a row search found, as of reads it, with the
// person a People row is named on it: it opens as their profile from the
// first frame (searchPeopleHits.ts), not through People's own page, which
// no reader's page list holds and which the app first asked about as a row.
func (locations *inlineDatabaseLocations) ofRow(row *workspaceSearchRowResult) (inlineDatabaseLocation, error) {
	location, err := locations.of(row.ContainerPageID, row.DataSourceID)
	if err != nil || row.DataSourceID == "" {
		return location, err
	}
	row.Person, err = peopleRowPersonOf(locations.app, row.DataSourceID, row.RowID)
	return location, err
}
