package app

import (
	"encoding/json"
	"errors"
)

// An app's manifest, leafen-app.json, says who made it, where it shows and
// everything it may reach: people's data, outside addresses, devices, live
// rooms, storage, workers and their triggers, its actions, what it tells
// agents and whether guests may join. An administrator approves exactly
// that list; a version whose list differs is approved again.

type appManifest struct {
	Schema       int                   `json:"schema"`
	ID           string                `json:"id"`
	Slug         string                `json:"slug"`
	Version      string                `json:"version"`
	MinLeafen    string                `json:"minLeafen,omitempty"`
	Name         map[string]string     `json:"name"`
	Description  map[string]string     `json:"description,omitempty"`
	Developer    appManifestDeveloper  `json:"developer"`
	Icon         string                `json:"icon,omitempty"`
	Category     string                `json:"category,omitempty"`
	Tags         []string              `json:"tags,omitempty"`
	License      string                `json:"license,omitempty"`
	Surfaces     appManifestSurfaces   `json:"surfaces"`
	Data         []appManifestData     `json:"data,omitempty"`
	PersonAccess string                `json:"personAccess,omitempty"`
	Network      appManifestNetwork    `json:"network"`
	Device       []string              `json:"device,omitempty"`
	Realtime     *appManifestRealtime  `json:"realtime,omitempty"`
	KV           *appManifestKV        `json:"kv,omitempty"`
	Workers      []appManifestWorker   `json:"workers,omitempty"`
	Schedules    []appManifestSchedule `json:"schedules,omitempty"`
	Webhooks     []appManifestWebhook  `json:"webhooks,omitempty"`
	// Standard protocol servers the app runs for programs outside Leafen.
	Endpoints []appManifestEndpoint `json:"endpoints,omitempty"`
	Events    []appManifestEvent    `json:"events,omitempty"`
	Actions   []appManifestAction   `json:"actions,omitempty"`
	// Lines in Leafen's own menus (docs/app-platform.md 10-1).
	SlashItems  []appManifestSlashItem  `json:"slashItems,omitempty"`
	MenuActions []appManifestMenuAction `json:"menuActions,omitempty"`
	// How database cells of some types are shown and entered.
	PropertyEditors []appManifestPropertyEditor `json:"propertyEditors,omitempty"`
	Agent           *appManifestAgent           `json:"agent,omitempty"`
	Guests          *appManifestGuests          `json:"guests,omitempty"`
	PWA             *appManifestPWA             `json:"pwa,omitempty"`
	// Notices the app's workers send to the people who use it.
	Notifications bool `json:"notifications,omitempty"`
	// The app reads the organization's groups and which of the people who
	// may use it are in each, as a shared vault follows a group.
	Groups bool `json:"groups,omitempty"`
	// The screen compiles WebAssembly, as a document engine's converter does:
	// its frame's scripts may then be WebAssembly too (document apps
	// decision of 2026-10-10, 5). Without it the frame refuses to compile.
	WebAssembly bool `json:"webAssembly,omitzero"`
	// The screen runs scripts it makes as it goes — eval, inline scripts — as
	// an office editor does (script-eval decision of 2026-10-10, answer B):
	// in the frame with no origin it opens in, its sandbox and the addresses
	// it may reach as they are; the administrator sees it in the review as a
	// high risk.
	ScriptEval bool `json:"scriptEval,omitzero"`
	// What the administrator sets when installing, such as the address of a
	// mail server a socket names as {{setting:KEY}} (app_platform_settings.go).
	Settings []appManifestSetting `json:"settings,omitempty"`
	// What a person gives the app — a mail account — sealed by Leafen and
	// filled into the sockets and relay headers that name it
	// (app_platform_credentials.go).
	Credentials []appManifestCredential `json:"credentials,omitempty"`
}

type appManifestDeveloper struct {
	Name           string `json:"name"`
	URL            string `json:"url,omitempty"`
	KeyFingerprint string `json:"keyFingerprint,omitempty"`
}

type appManifestSurfaces struct {
	FullPage  *appManifestEntry  `json:"fullPage,omitempty"`
	Blocks    []appManifestBlock `json:"blocks,omitempty"`
	SidePanel *appManifestEntry  `json:"sidePanel,omitempty"`
	// Files a person opens with the app from a file block's menu (document
	// apps decision of 2026-10-10, 5).
	FileOpeners []appManifestFileOpener `json:"fileOpeners,omitempty"`
	// Tools the app moves content from, each a card on the import screen
	// that opens its screen (docs/app-platform.md 10-1).
	ImportSources []appManifestImportSource `json:"importSources,omitempty"`
}

// appManifestImportSource is a tool the app moves content from: its card on
// the import screen and the screen it opens, which hands Leafen a workspace
// bundle (docs/bundle.md).
type appManifestImportSource struct {
	Key         string            `json:"key"`
	Name        map[string]string `json:"name"`
	Description map[string]string `json:"description"`
	Entry       string            `json:"entry"`
}

// appManifestFileOpener opens files of some types — by extension, ".docx",
// or by type, "application/pdf" — with one of the app's screens.
type appManifestFileOpener struct {
	Key   string            `json:"key"`
	Name  map[string]string `json:"name"`
	Types []string          `json:"types"`
	Entry string            `json:"entry"`
	// The screen edits the file and saves it back, one person at a time.
	Edit bool `json:"edit,omitzero"`
	// The blank files of its types the screen starts from, for the
	// library's "새 문서" (document apps decision of 2026-10-11, 12).
	NewFiles []appManifestNewFile `json:"newFiles,omitempty"`
}

// appManifestNewFile is a blank file a file opener starts from: one of the
// opener's extensions, what the file is called in "새 문서", and the file of
// the package Leafen copies into a page and opens with the opener.
type appManifestNewFile struct {
	Type     string            `json:"type"`
	Name     map[string]string `json:"name"`
	Template string            `json:"template"`
}

type appManifestEntry struct {
	Entry string `json:"entry"`
}

type appManifestBlock struct {
	Key           string            `json:"key"`
	Name          map[string]string `json:"name"`
	Entry         string            `json:"entry"`
	DefaultHeight int               `json:"defaultHeight,omitzero"`
}

type appManifestData struct {
	Key       string `json:"key"`
	File      string `json:"file"`
	Locked    bool   `json:"locked,omitempty"`
	Hidden    bool   `json:"hidden,omitempty"`
	PerPerson bool   `json:"perPerson,omitzero"`
}

type appManifestNetwork struct {
	Direct  []appManifestDirect `json:"direct,omitempty"`
	Relay   []appManifestRelay  `json:"relay,omitempty"`
	Sockets []appManifestSocket `json:"sockets,omitempty"`
	OAuth   []appManifestOAuth  `json:"oauth,omitempty"`
}

type appManifestDirect struct {
	URL   string   `json:"url"`
	Kinds []string `json:"kinds"`
}

type appManifestRelay struct {
	URL            string   `json:"url"`
	Secrets        []string `json:"secrets,omitempty"`
	PrivateNetwork bool     `json:"privateNetwork,omitzero"`
	// The outside sign-ins whose tokens may go to this address.
	OAuth []string `json:"oauth,omitempty"`
	// The people's credentials that may go to this address.
	Credentials []string `json:"credentials,omitempty"`
}

type appManifestSocket struct {
	Host           string `json:"host"`
	Port           int    `json:"port,omitzero"`
	TLS            bool   `json:"tls"`
	PrivateNetwork bool   `json:"privateNetwork,omitzero"`
	// The people's credentials that may be written to this socket.
	Credentials []string `json:"credentials,omitempty"`
	// The setting an administrator-set address came from, once resolved
	// (appManifestWithSettings); a worker opens it as {{setting:KEY}}.
	Setting string `json:"-"`
}

type appManifestOAuth struct {
	Key          string   `json:"key"`
	AuthorizeURL string   `json:"authorizeUrl"`
	TokenURL     string   `json:"tokenUrl"`
	Scopes       []string `json:"scopes,omitempty"`
	// Whose sign-in it is: each person's own (the default) or one for the app.
	Owner string `json:"owner,omitempty"`
	// More of the provider's own sign-in parameters, such as Google's
	// access_type=offline for a token that renews.
	AuthorizeParams map[string]string `json:"authorizeParams,omitempty"`
}

// An app's open rooms on the server: 50 unless it declares up to 500.
const (
	appRoomsPerAppDefault = 50
	appRoomsPerAppMax     = 500
)

type appManifestRealtime struct {
	MaxParticipants int  `json:"maxParticipants"`
	WebRTC          bool `json:"webrtc,omitzero"`
	// MaxRooms is how many of its rooms may be open at once on the server
	// (1–500, 50 when not given).
	MaxRooms int `json:"maxRooms,omitzero"`
	// The worker function that answers messages a room sends the server.
	Worker   string `json:"worker,omitempty"`
	Function string `json:"function,omitempty"`
	// Loop runs the function once for each room while people are in it,
	// reading the room's messages and sending to the room as it goes.
	Loop bool `json:"loop,omitzero"`
}

type appManifestKV struct {
	MaxBytes      int64 `json:"maxBytes"`
	MaxTTLSeconds int64 `json:"maxTtlSeconds"`
}

type appManifestWorker struct {
	Name           string   `json:"name"`
	Module         string   `json:"module"`
	Functions      []string `json:"functions"`
	MemoryMB       int      `json:"memoryMb,omitzero"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitzero"`
	LongRunning    bool     `json:"longRunning,omitzero"`
	// Run is the function a long-running worker keeps running in each
	// organization the app has a workspace in.
	Run string `json:"run,omitempty"`
	// Resident keeps the worker's instance between its endpoints' calls, so
	// a large program starts once and may keep what it read in memory; what
	// lasts belongs in Leafen.
	Resident bool `json:"resident,omitzero"`
}

type appManifestSchedule struct {
	Key       string `json:"key"`
	Cron      string `json:"cron"`
	Worker    string `json:"worker"`
	Function  string `json:"function"`
	PerPerson bool   `json:"perPerson,omitzero"`
	// Name is how people see a per-person schedule they may turn off.
	Name map[string]string `json:"name,omitempty"`
	// TimeZone is the IANA zone the cron runs in; without it the server's,
	// or each person's own for a per-person schedule.
	TimeZone string `json:"timeZone,omitempty"`
}

type appManifestWebhook struct {
	Key      string `json:"key"`
	Worker   string `json:"worker"`
	Function string `json:"function"`
	Verify   string `json:"verify"`
	// Name is how people see the webhook, in languages (app_platform_webhook_names.go).
	Name map[string]string `json:"name,omitempty"`
}

// appManifestEndpoint is a standard protocol server — WebDAV, CalDAV, a
// Bitwarden-compatible API — the app answers at its address under Leafen's
// (/app-endpoints/<slug>/<key>/), every path and method below it going to the
// worker function, for programs outside Leafen signed in with the person's
// app password (document apps decision of 2026-10-10, 8). Methods narrows
// what it takes; Public lets it answer without signing in, once an
// administrator allows that.
type appManifestEndpoint struct {
	Key      string            `json:"key"`
	Name     map[string]string `json:"name"`
	Worker   string            `json:"worker"`
	Function string            `json:"function"`
	Methods  []string          `json:"methods,omitempty"`
	Public   bool              `json:"public,omitzero"`
	// FileBodies are the methods whose bodies always come to the worker as
	// a stored file, however small — a file server's uploads.
	FileBodies []string `json:"fileBodies,omitempty"`
	// Pages lets a public endpoint's worker answer with its package's site/
	// files, a web app of its own beside its API (app_platform_endpoint_pages.go).
	Pages bool `json:"pages,omitempty"`
}

// appEndpointMethods are the methods an endpoint may name: HTTP's own and
// WebDAV's, CalDAV's and CardDAV's.
var appEndpointMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "PROPFIND", "PROPPATCH", "MKCOL",
	"COPY", "MOVE", "LOCK", "UNLOCK", "REPORT", "MKCALENDAR", "SEARCH", "ACL"}

// appManifestEvent runs a worker function when the app's workspace changes
// in one of the event kinds Leafen's event webhooks know.
type appManifestEvent struct {
	Key      string   `json:"key"`
	Types    []string `json:"types"`
	Worker   string   `json:"worker"`
	Function string   `json:"function"`
	// DataSource narrows it to the rows of one of the app's data sources
	// (its data key); Properties to changes of the properties named, and
	// Where to rows whose values match.
	DataSource string              `json:"dataSource,omitempty"`
	Properties []string            `json:"properties,omitempty"`
	Where      []appEventCondition `json:"where,omitempty"`
}

// appEventCondition holds when the row's property is, isn't, or is (not)
// empty — by the value as it reads: an option's name, true or false, the
// text, the number, a date's start.
type appEventCondition struct {
	Property  string  `json:"property"`
	Equals    *string `json:"equals,omitempty"`
	NotEquals *string `json:"notEquals,omitempty"`
	Empty     *bool   `json:"empty,omitempty"`
}

type appManifestAction struct {
	Key              string            `json:"key"`
	Name             map[string]string `json:"name"`
	Description      map[string]string `json:"description,omitempty"`
	Input            json.RawMessage   `json:"input,omitempty"`
	RunAs            string            `json:"runAs"`
	Worker           string            `json:"worker"`
	Function         string            `json:"function"`
	Risk             string            `json:"risk"`
	RequiresApproval bool              `json:"requiresApproval,omitzero"`
	ExternalEffect   bool              `json:"externalEffect,omitzero"`
	Undo             string            `json:"undo,omitempty"`
	// Tool false keeps the action from agents; every other action is theirs
	// too (offeredToAgents).
	Tool *bool `json:"tool,omitempty"`
	// InputLabels names the input's fields in each language for the
	// person approving the action (docs/app-platform.md 5).
	InputLabels map[string]map[string]string `json:"inputLabels,omitempty"`
}

// offeredToAgents says whether agents may call the action: what a person
// does on the app's screen an agent does too (agent full-permission decision
// of 2026-10-16, 6), unless the app keeps it with "tool": false.
func (action appManifestAction) offeredToAgents() bool {
	return action.Tool == nil || *action.Tool
}

// appManifestSlashItem is a "/" menu item: it runs one of the app's
// actions or puts one of its blocks in the page.
type appManifestSlashItem struct {
	Key      string            `json:"key"`
	Name     map[string]string `json:"name"`
	Keywords []string          `json:"keywords,omitempty"`
	Action   string            `json:"action,omitempty"`
	Block    string            `json:"block,omitempty"`
}

// appManifestMenuAction is a line in a database row's or a page's "…" menu
// that runs one of the app's actions on that row or page.
type appManifestMenuAction struct {
	Key    string            `json:"key"`
	Name   map[string]string `json:"name"`
	On     []string          `json:"on"`
	Action string            `json:"action"`
}

// appManifestPropertyEditor draws a database cell of the types it names
// and enters its value, which keeps its own type.
type appManifestPropertyEditor struct {
	Key   string            `json:"key"`
	Name  map[string]string `json:"name"`
	Types []string          `json:"types"`
	Entry string            `json:"entry"`
	// The editor's height in pixels while a cell is edited.
	Height int `json:"height,omitempty"`
}

type appManifestAgent struct {
	Summary      map[string]string     `json:"summary,omitempty"`
	Instructions map[string]string     `json:"instructions,omitempty"`
	Resources    []appManifestResource `json:"resources,omitempty"`
	Prompts      []appManifestPrompt   `json:"prompts,omitempty"`
}

type appManifestResource struct {
	Key    string      `json:"key"`
	Name   appNameText `json:"name"`
	Action string      `json:"action"`
}

type appManifestPrompt struct {
	Key      string      `json:"key"`
	Name     appNameText `json:"name"`
	Template string      `json:"template"`
}

// appNameText is a name agents read: text, or like the app's other names an
// object of language codes to text, read in English or else its first
// language (2026-10-16: only these names refused the object form).
type appNameText string

func (name *appNameText) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*name = appNameText(text)
		return nil
	}
	languages := map[string]string{}
	if json.Unmarshal(raw, &languages) != nil {
		return errors.New(`an agent prompt's or resource's name is text or an object of language codes to text, such as {"en": "…"}`)
	}
	*name = appNameText(appPermissionName(languages))
	return nil
}

type appManifestGuests struct {
	Rooms []string `json:"rooms"`
	// Names are each room's name in languages, as the app's screen calls it
	// (app_platform_guest_rooms.go).
	Names          map[string]map[string]string `json:"names,omitempty"`
	ResultDatabase string                       `json:"resultDatabase,omitempty"`
	MaxPerRoom     int                          `json:"maxPerRoom"`
}

type appManifestPWA struct {
	ShortName  string   `json:"shortName,omitempty"`
	ThemeColor string   `json:"themeColor,omitempty"`
	Offline    []string `json:"offline,omitempty"`
}
