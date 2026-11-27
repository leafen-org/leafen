package app

import (
	"encoding/json"
	"maps"
	"strings"
)

// A database may show its own sources and sources linked from other
// databases together. Own members are dataSourceId followed by
// additionalDataSourceIds; linked members follow in linkedMembers, each naming
// the database that owns the source. A linked database's first member is the
// linked dataSourceId itself. Blocks without linkedMembers keep their meaning.

type databaseContainerLinkedMember struct {
	DataSourceID      string `json:"dataSourceId"`
	SourceContainerID string `json:"sourceContainerId"`
}

// memberDataSourceIDs is every member in order: own members, then linked
// members.
func (content databaseContainerBlockContent) memberDataSourceIDs() []string {
	members := content.dataSourceIDs()
	for _, member := range content.LinkedMembers {
		members = append(members, member.DataSourceID)
	}
	return members
}

// linkedSourceContainers maps each linked member source to the database that
// owns it, including a linked database's own first member.
func (content databaseContainerBlockContent) linkedSourceContainers() map[string]string {
	linked := map[string]string{}
	if content.LinkedDatabaseSource && content.DataSourceID != "" {
		linked[content.DataSourceID] = content.SourceContainerID
	}
	for _, member := range content.LinkedMembers {
		linked[member.DataSourceID] = member.SourceContainerID
	}
	return linked
}

// ownDataSourceIDs is the sources the database owns, in order.
func (content databaseContainerBlockContent) ownDataSourceIDs() []string {
	if content.LinkedDatabaseSource {
		return nil
	}
	return content.dataSourceIDs()
}

// linkedMemberList is every linked member in order, a linked database's first
// member included.
func (content databaseContainerBlockContent) linkedMemberList() []databaseContainerLinkedMember {
	members := []databaseContainerLinkedMember{}
	if content.LinkedDatabaseSource && content.DataSourceID != "" {
		members = append(members, databaseContainerLinkedMember{
			DataSourceID: content.DataSourceID, SourceContainerID: content.SourceContainerID,
		})
	}
	return append(members, content.LinkedMembers...)
}

// withDatabaseContainerMembers rewrites stored block content to the given own
// and linked members and keeps its other fields. A database left without own
// sources becomes a linked database of its first linked member.
func withDatabaseContainerMembers(
	content map[string]any, own []string, linked []databaseContainerLinkedMember,
) map[string]any {
	next := maps.Clone(content)
	for _, key := range []string{
		"dataSourceId", "additionalDataSourceIds", "linkedDatabaseSource", "sourceContainerId", "linkedMembers",
	} {
		delete(next, key)
	}
	if len(own) > 0 {
		next["dataSourceId"] = own[0]
		if len(own) > 1 {
			next["additionalDataSourceIds"] = own[1:]
		}
	} else if len(linked) > 0 {
		next["dataSourceId"], next["linkedDatabaseSource"] = linked[0].DataSourceID, true
		next["sourceContainerId"] = linked[0].SourceContainerID
		linked = linked[1:]
	}
	if len(linked) > 0 {
		next["linkedMembers"] = linked
	}
	withDatabaseTrashedSources(next, own, storedDatabaseTrashedSources(content))
	return next
}

// validateDatabaseContainerLinkedMembers checks the linked member list shape.
func validateDatabaseContainerLinkedMembers(raw map[string]json.RawMessage, content databaseContainerBlockContent) error {
	encoded, present := raw["linkedMembers"]
	if !present {
		return nil
	}
	members := []databaseContainerLinkedMember{}
	if err := json.Unmarshal(encoded, &members); err != nil || members == nil {
		return graphInputError(errorCodeGraphInvalid, "Check the database's linked data source list.")
	}
	seen := map[string]struct{}{}
	for _, id := range content.dataSourceIDs() {
		seen[id] = struct{}{}
	}
	for _, member := range members {
		for _, id := range []string{member.DataSourceID, member.SourceContainerID} {
			if id == "" || strings.TrimSpace(id) != id || len(id) > maxGraphIdentifierLength {
				return graphInputError(errorCodeGraphInvalid, "Check the database's linked data source list.")
			}
		}
		if _, duplicate := seen[member.DataSourceID]; duplicate {
			return graphInputError(
				errorCodeGraphReferenceInvalid, "A database can't contain the same data source twice.",
			)
		}
		seen[member.DataSourceID] = struct{}{}
	}
	return nil
}
