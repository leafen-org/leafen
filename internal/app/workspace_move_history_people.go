package app

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Who a moved workspace's history names here (workspace_move_history.go): a
// person paired the account here, one not paired the stand-in the content
// names, `leafen-user:<their ID>`, whose name the workspace keeps for its
// versions to show.

// workspaceMoveHistoryPeople is who each person the history names is here.
type workspaceMoveHistoryPeople map[string]string

func workspaceMoveHistoryPeopleOf(people []workspaceImportPerson) workspaceMoveHistoryPeople {
	named := workspaceMoveHistoryPeople{}
	for _, person := range people {
		if person.SourceID == "" {
			continue
		}
		if person.TargetID != "" {
			named[person.SourceID] = person.TargetID
		} else {
			named[person.SourceID] = workspaceImportPersonPrefix + person.SourceID
		}
	}
	return named
}

func (people workspaceMoveHistoryPeople) person(value string) (string, bool) {
	next, ok := people[value]
	return next, ok && next != value
}

func (people workspaceMoveHistoryPeople) id(value string) string {
	if next, ok := people.person(value); ok {
		return next
	}
	return value
}

// names reports whether raw may hold a person to rename: none is named
// verbatim in it, as JSON and Ygo both write strings.
func (people workspaceMoveHistoryPeople) names(raw []byte) bool {
	if len(people) > 64 {
		return true
	}
	for source := range people {
		if bytes.Contains(raw, []byte(source)) {
			return true
		}
	}
	return false
}

// json renames the people in a JSON value: its strings and its keys.
func (people workspaceMoveHistoryPeople) json(raw []byte) ([]byte, error) {
	if len(raw) == 0 || !people.names(raw) {
		return raw, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return people.encode(people.walk(value))
}

func (people workspaceMoveHistoryPeople) walk(value any) any {
	switch typed := value.(type) {
	case string:
		return people.id(typed)
	case []any:
		for index, item := range typed {
			typed[index] = people.walk(item)
		}
	case map[string]any:
		renamed := map[string]any{}
		for key, item := range typed {
			if next, ok := people.person(key); ok {
				delete(typed, key)
				renamed[next] = people.walk(item)
				continue
			}
			typed[key] = people.walk(item)
		}
		for key, item := range renamed {
			typed[key] = item
		}
	}
	return value
}

func (people workspaceMoveHistoryPeople) encode(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// ygo renames the people in a Ygo update or state.
func (people workspaceMoveHistoryPeople) ygo(update []byte) ([]byte, error) {
	if len(update) == 0 || !people.names(update) {
		return update, nil
	}
	return crdt.RewriteUpdateV1(update, people.person)
}

// stored renames the people in a kept column, unpacking and packing it again.
func (people workspaceMoveHistoryPeople) stored(value []byte, rename func([]byte) ([]byte, error)) ([]byte, error) {
	raw, err := unpackPageHistory(value)
	if err != nil {
		return nil, err
	}
	if !people.names(raw) {
		return value, nil
	}
	renamed, err := rename(raw)
	if err != nil {
		return nil, err
	}
	return packPageHistory(renamed), nil
}

// storedText is stored for a text column: packed content goes in as bytes.
func (people workspaceMoveHistoryPeople) storedText(value []byte) (any, error) {
	renamed, err := people.stored(value, people.json)
	if err != nil || isPackedPageHistory(renamed) {
		return renamed, err
	}
	return string(renamed), nil
}

// event renames the people in an event's payload: its envelope's JSON and
// its update, or an update alone from before envelopes. The envelope keeps
// the update in base64, where no name reads verbatim, so it is always read.
func (people workspaceMoveHistoryPeople) event(payload []byte) ([]byte, error) {
	raw, err := unpackPageHistory(payload)
	if err != nil {
		return nil, err
	}
	envelope := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&envelope) != nil || envelope["format"] == nil {
		return people.stored(payload, people.ygo)
	}
	update := []byte{}
	if encoded, ok := envelope["update"].(string); ok {
		if err := json.Unmarshal([]byte(`"`+encoded+`"`), &update); err != nil {
			return nil, err
		}
	}
	if !people.names(raw) && !people.names(update) {
		return payload, nil
	}
	delete(envelope, "update")
	people.walk(envelope)
	renamed, err := people.ygo(update)
	if err != nil {
		return nil, err
	}
	envelope["update"] = renamed
	encoded, err := people.encode(envelope)
	if err != nil {
		return nil, err
	}
	return packPageHistory(encoded), nil
}

// keepWorkspaceMovedPeople keeps the names of a moved workspace's people
// who weren't paired, by the stand-in IDs its content and history give them.
func keepWorkspaceMovedPeople(app core.App, workspaceID string, people []workspaceImportPerson) error {
	for _, person := range people {
		if person.SourceID == "" || person.TargetID != "" {
			continue
		}
		if _, err := app.DB().NewQuery(`INSERT OR REPLACE INTO {{leafen_workspace_moved_people}}
			([[workspace_id]], [[person_id]], [[name]], [[email]]) VALUES ({:workspace}, {:person}, {:name}, {:email})`).Bind(dbx.Params{
			"workspace": workspaceID, "person": workspaceImportPersonPrefix + person.SourceID,
			"name": strings.TrimSpace(person.Name), "email": strings.ToLower(strings.TrimSpace(person.Email)),
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// workspaceMovedPeopleProfiles are the stand-ins among actorIDs, by the
// names their moved workspaces keep.
func workspaceMovedPeopleProfiles(app core.App, actorIDs []string, known map[string]bool) ([]pageHistoryActorProfile, error) {
	profiles := []pageHistoryActorProfile{}
	if !tableKnown(app, "leafen_workspace_moved_people") {
		return profiles, nil
	}
	for _, actorID := range actorIDs {
		if known[actorID] || !strings.HasPrefix(actorID, workspaceImportPersonPrefix) {
			continue
		}
		name := ""
		if err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_workspace_moved_people}} WHERE [[person_id]] = {:person} LIMIT 1`).
			Bind(dbx.Params{"person": actorID}).Row(&name); err != nil || name == "" {
			continue
		}
		known[actorID] = true
		profiles = append(profiles, pageHistoryActorProfile{UserID: actorID, DisplayName: name})
	}
	return profiles, nil
}
