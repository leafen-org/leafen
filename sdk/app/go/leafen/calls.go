package leafen

import "encoding/json"

// What each trigger gives a worker function as its input.

// ActionCall is an action run from the app's screen, or an agent's call of
// the action as a tool (then Via says which connection).
type ActionCall struct {
	Action string          `json:"action"`
	Input  json.RawMessage `json:"input"`
	// The person who ran it, and the organization it ran in.
	Person  string `json:"person"`
	Tenancy string `json:"tenancy"`
	// Their role in that organization (owner, admin, member, guest…) and the
	// admin roles they hold there (people, it, compliance, analytics,
	// billing), as Leafen's settings know them.
	Role       string   `json:"role,omitempty"`
	AdminRoles []string `json:"adminRoles,omitempty"`
	Via        *struct {
		Kind       string `json:"kind"`
		Connection string `json:"connection"`
		Name       string `json:"name"`
	} `json:"via,omitempty"`
}

// HookCall is a call of one of the app's incoming webhooks.
type HookCall struct {
	Hook        string `json:"hook"`
	Delivery    string `json:"delivery"`
	ContentType string `json:"contentType"`
	Body        []byte `json:"body"`
	ReceivedAt  int64  `json:"receivedAt"`
}

// ScheduleCall is one turn of a schedule, for an organization and, when the
// schedule runs per person, a person.
type ScheduleCall struct {
	Schedule string `json:"schedule"`
	At       int64  `json:"at"`
	Tenancy  string `json:"tenancy"`
	Person   string `json:"person"`
}

// EventCall is a change in the app's workspace of a kind its manifest's
// events list.
type EventCall struct {
	Event  string `json:"event"`
	Change struct {
		Type        string          `json:"type"`
		EntityID    string          `json:"entityId"`
		EntityType  string          `json:"entityType"`
		PageID      string          `json:"pageId"`
		Actor       string          `json:"actor"`
		WorkspaceID string          `json:"workspaceId"`
		Data        json.RawMessage `json:"data"`
	} `json:"change"`
	Tenancy    string `json:"tenancy"`
	ReceivedAt int64  `json:"receivedAt"`
}

// RoomCall is a message a realtime room sent the server; the function's
// output, if any, goes to the whole room.
type RoomCall struct {
	Room    string `json:"room"`
	Tenancy string `json:"tenancy"`
	Message string `json:"message"`
}

// Action reads an action's call and decodes its input into target.
func Action(target any) (ActionCall, error) {
	call := ActionCall{}
	if err := InputJSON(&call); err != nil {
		return call, err
	}
	if target != nil && len(call.Input) > 0 {
		return call, json.Unmarshal(call.Input, target)
	}
	return call, nil
}
