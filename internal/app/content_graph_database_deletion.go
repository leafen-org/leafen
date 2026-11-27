package app

import (
	"sort"

	"github.com/reearth/ygo/crdt"
)

func databaseEntityIDsForSource(doc *crdt.Doc, rootName, sourceID string) ([]string, error) {
	ids := []string{}
	for id, raw := range doc.GetMap(rootName).Entries() {
		entity, ok := raw.(map[string]any)
		if !ok {
			return nil, graphStructureAuthorityError()
		}
		if entity["dataSourceId"] == sourceID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
