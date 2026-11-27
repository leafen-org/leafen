package app

import (
	"encoding/json"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Read only the first three property definitions of authorized cards. The
// thumbnail uses metadata and option examples, not private row content.
func fillDatabaseContainerPickerPreviews(app core.App, response *databaseContainerPickerResponse) error {
	ids := []string{}
	seen := map[string]bool{}
	for _, item := range response.Items {
		if !seen[item.DataSourceID] {
			ids = append(ids, item.DataSourceID)
			seen[item.DataSourceID] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(ids)
	properties := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
  SELECT [[property]].[[id]],[[property]].[[data_source_id]],[[property]].[[type]],[[property]].[[name]],[[property]].[[position]],[[property]].[[config]]
  FROM json_each({:ids}) [[source]]
  JOIN {{leafen_data_sources}} [[definition]] ON [[definition]].[[id]]=[[source]].[[value]]
  JOIN json_each((SELECT json_group_array([[id]]) FROM (
   SELECT [[id]] FROM {{leafen_database_properties}}
   WHERE [[owner]]=[[definition]].[[owner]] AND [[page_id]]=[[definition]].[[page_id]] AND [[data_source_id]]=[[source]].[[value]]
   ORDER BY [[position]],[[id]] LIMIT 3
  ))) [[chosen]]
  JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]]=[[chosen]].[[value]]
  ORDER BY [[source]].[[key]],[[property]].[[position]],[[property]].[[id]]
 `).Bind(dbx.Params{"ids": string(encoded)}).All(&properties); err != nil {
		return err
	}
	bySource := map[string][]graphProperty{}
	for _, property := range properties {
		bySource[property.DataSourceID] = append(bySource[property.DataSourceID], graphProperty{ID: property.ID, DataSourceID: property.DataSourceID, Type: property.Type, Name: property.Name, Position: property.Position, Config: json.RawMessage(property.Config)})
	}
	for index := range response.Items {
		response.Items[index].PreviewProperties = bySource[response.Items[index].DataSourceID]
	}
	return nil
}
