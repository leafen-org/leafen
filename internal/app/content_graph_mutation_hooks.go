package app

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// What a caller of a page's content graph mutation hooks into it — who may
// change the page, how its input and document are prepared and checked — and
// what preparing the mutation leaves for applying it (content_graph_mutation_owner.go).
type contentGraphMutationHooks struct {
	Authorize             func(core.App) (pageAuthorization, error)
	PrepareInput          func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error)
	BeforeApply           func(core.App, pageAuthorization, *crdt.Doc) error
	BeforeApplyOperations func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error
	ApplyAdditionalText   func(core.App, pageAuthorization) (string, error)
	ResolveRequestHash    func(core.App, pageAuthorization, *crdt.Doc) (string, error)
	Prepare               func(core.App, pageAuthorization, *crdt.Doc) (contentGraphMutationPreparation, error)
	AfterApply            func(core.App, pageAuthorization, *crdt.Doc) error
	// CheckOperations checks a change's operations just before they apply
	// (before BeforeApplyOperations): it refuses them, or settles what they
	// write themselves — a column's options marked deleted taken out of its
	// settings (database_option_removal.go) — and changes nothing else, so
	// the change says what its operations changed as it would without it
	// (content_graph_changed_rows.go). Hooked as BeforeApplyOperations, the
	// options' check had every change of a page's content and of rows'
	// values say nothing: a page of 5,000 rows behind one value read its 51
	// windows again, some 7 seconds on a NAS-class server (2026-11-27).
	CheckOperations func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error
	// StepsKeepGraph says the write's own steps beside its operations
	// (BeforeApplyOperations, AfterApply) change nothing of the page's graph
	// its operations don't name: they read, keep the write's own record and
	// answer, write on the rows it names only what a read of those rows
	// takes in (an icon, an archive), or record a change of their own
	// (announceDatabaseRowPageChange). The change then says what its
	// operations changed, as a person's change of the same operations says
	// it (content_graph_changed_rows.go): a button's run
	// (interactive_automation_execution.go), a row's values written through
	// the Notion-compatible API or MCP (notion_database_row_page_writes.go),
	// a page's body written so (notion_page_markdown_mutation.go,
	// notion_block_mutation.go), a row made from a template or through the
	// API (database_toolbar_template_row.go, notion_database_row_page_writes.go),
	// a form's answer (form_submission.go), a sprint's change
	// (database_sprint_actions.go), a view written with its form's link
	// (mcp_tool_view.go, notion_view_mutation.go), and a connection's
	// authorship around its writes (connection_authorship.go).
	StepsKeepGraph   bool
	TextSourceCopies map[string]string
	// StepsChangeGraph, given with steps beside a write's operations that
	// change the page's graph for some writes only, says once they ran
	// whether this one's did — a data source's change sending a property to
	// Deleted properties takes its cells; one adding an option, renaming a
	// property or the source doesn't — and steps that didn't keep the
	// change's record of what its operations changed, as StepsKeepGraph
	// does (content_graph_changed_rows.go). Before, an agent's and the API's
	// every change of a data source had a page behind it read whole: 51
	// windows, some 7 seconds on a NAS-class server, for a database of 5,000
	// rows (2026-11-27).
	StepsChangeGraph func() bool
	// HistoryRows are row changes the write makes other than by its operations.
	HistoryRows []pageHistoryRowChange
	// ChangedRows names, after a write of no operations is made, the rows
	// its own steps (AfterApply) changed when they changed nothing else of
	// the page's graph — a row put in the trash or brought back — so a page
	// behind it reads those alone (content_graph_changed_rows.go).
	ChangedRows func() []string
	// LiftedTextBlockIDs are the blocks whose text updates came from their
	// content (content_graph_block_text_lift.go): text the page already has wins.
	LiftedTextBlockIDs  map[string]struct{}
	ReplaceExistingText bool
	// TextPieces are texts the write leaves as they are but for the piece
	// marks that keep them in the pieces given (graphTextKeptPieceFormats).
	TextPieces []graphTextUpdate
	// SearchFromNamedBlocks says the write changes the page's text only
	// through its operations and text updates — its other steps record or
	// answer, not edit — so search makes those blocks again, not the whole
	// page (workspace_search_dirty_blocks.go).
	SearchFromNamedBlocks          bool
	StructuralAppend               bool
	ExplicitStartBlockIDs          map[string]struct{}
	WorkspaceImportReplaceJobID    string
	WorkspaceImportReplaceRollback bool
	AllowBlockUpdateBeforeDelete   bool
	// StoredBlocks marks a change bringing back blocks as they were stored —
	// an export's import, a trash entry's restore, a history revert — whose
	// blocks a write left under a synced copy before 2026-11-21 come back
	// with the rest (validateStructuralGraphPlaces), to go where they are
	// seen after it (applyStoredContentGraphMutation).
	StoredBlocks bool
	// SuppressAutomationEvents keeps system rewrites, such as property type
	// conversion, from firing row automations or notifications.
	SuppressAutomationEvents bool
	// AutomationWrite is a database automation's write to another page's
	// database, which sets off none of that database's automations
	// (prepareDatabaseAutomationEvents) and still tells the rows' followers.
	AutomationWrite bool
	// FormRespondent is who a form's answer written with another's access is
	// by, told in its notices and the page's history; none is "익명".
	FormRespondent string
	// FormResponse marks a form's answer, which sets off the database's
	// automations even from someone signed out: they run with their own
	// saver's rights, and no one is "whoever triggered".
	FormResponse bool
	// SuppressNotifications keeps imported content from notifying the people
	// its rows name.
	SuppressNotifications bool
	// LeafenOwn marks Leafen's own upkeep of the pages it keeps under their
	// owner's authority — the workspace's People and its people's pages made
	// and kept as its people are (people_database.go), and an invitee's
	// values turned into their account (people_value_invitee_runner.go): no
	// one's act, from its first step (content_graph_mutation_leafen_own.go).
	// Its versions are Leafen's in the pages' histories, its rows' pages'
	// among them, and it makes no line in the
	// workspace's updates (2026-11-16: a member's updates listed People's
	// making and each person's page as made by the workspace's owner), no
	// page interaction of its owner's — a profile's activity and collaborators,
	// a legal hold's history and its workspace's interaction revision read
	// none — and no event for a webhook or an app (2026-11-17: the owner's
	// webhook heard of each person's page as made, and a new workspace's
	// profile listed People as edited by its owner). A person's edit of those
	// pages is theirs, listed and told as any other.
	LeafenOwn bool
	// PreserveRowEditStamps marks the system's rewrite of rows (numbering an
	// ID, converting a property's type, mirroring a two-way relation on its
	// other side, moving data to a new model), which keeps each row's last
	// edited time and editor.
	PreserveRowEditStamps bool
	// CommentMarks marks a write that only marks a value's words with the
	// discussion left on them (comment_cell_anchors.go): the page's and its
	// rows' histories keep it as no edit (pageHistoryVersionCommentMarks).
	CommentMarks bool
	// PagesMadeRows are the pages a change makes its database's pages as they
	// are — a page's pages turned into its wiki's (wiki_database_job.go):
	// none is made, so none is a made line in the workspace's updates
	// (recordWorkspaceRowActivities).
	PagesMadeRows []string
	// WikiHomeMade is the data source of the wiki whose Home the change makes
	// with its database (wiki_database.go): one of the wiki's views in its
	// updates (recordWorkspaceDatabaseActivities).
	WikiHomeMade string
	// SkipRelationPairSync keeps a mirrored two-way relation change from
	// recording work back to the side it came from.
	SkipRelationPairSync bool
	// RelationPairsAsWritten writes two-way relations as the change has both
	// their sides, with no work on the other side: an archive's import carries
	// each side as it was, even one a copy left naming a side that doesn't
	// name it back, which that work would take over.
	RelationPairsAsWritten bool
	// MirrorsRelationPair marks the write of a two-way relation's other side
	// that a change to a row of another data source asked for: it reaches
	// rows its causer may not change themselves — a page they only view or
	// cannot see, a property its access rules keep from them — as Notion's
	// does (seen 2026-11-09), so what their own
	// edit of those rows meets does not stop it.
	MirrorsRelationPair bool
	// SettingsAsAsked gives a property whose type the write changes just the
	// settings asked for, as Notion's API does; the Notion-compatible API's
	// data source writes ask for it. Made a select or multi-select, it has
	// just the options listed (database_property_conversion_listed.go);
	// changed back to a type it had, it doesn't take back that type's
	// settings (recordDatabasePropertyConversion).
	SettingsAsAsked bool
	// KeepTrashedRowsInStep lets the system's rewrite of a property's cells
	// (converting its type) reach rows in the trash too, so one restored later
	// reads as its database does; people cannot change such a row.
	KeepTrashedRowsInStep bool
	// AllowTrashedPage lets system work (imports, relation mirroring, people
	// mapping) reach a page in the trash; people cannot change such a page.
	AllowTrashedPage bool
	// EnforcePageLock marks a change made in the app, which a page lock
	// stops for everything but database rows.
	EnforcePageLock bool
	// PersonChange marks a person's change, in the app or as Markdown written
	// to a page, where a page block made here makes its page, deleting one
	// sends its page to the trash, and a database's new Verification property
	// comes with its Owner (database_verification_properties.go).
	PersonChange bool
	// LinkVerificationProperties gives a database's new Verification property
	// its Owner as a person's change does, for the public API's data sources.
	LinkVerificationProperties bool
	// RemoveWiki lets undoing a wiki take away its database, which other
	// changes leave with the wiki.
	RemoveWiki bool
	// DateCommandRowID binds a row-only editor's date command to its own row.
	DateCommandRowID string
	// Words is the page's collaborative text written with the change, one
	// Yjs update its editor held for it, stored with it as the page's socket
	// stores text (content_graph_mutation_words.go).
	Words []byte
	// Typing is the page's collaborative text its editor wrote before the
	// change, which waited for the block changes before it and goes with
	// this one: stored first, as the typing it is (storeGraphMutationTyping).
	Typing []byte
	// TellRoom sends the page's open room the blocks a change of blocks
	// alone made, with its words, for the pages open there to draw them
	// without a read (content_graph_room_change.go).
	TellRoom bool
}

type contentGraphMutationPreparation struct {
	Operations                 []graphOperation
	RuntimeWritablePropertyIDs map[string]struct{}
	Deferred                   bool
}

// beforeOperations runs a change's steps just before its operations apply:
// its check (CheckOperations), then its own step (BeforeApplyOperations).
func (hooks *contentGraphMutationHooks) beforeOperations(
	app core.App, authorization pageAuthorization, doc *crdt.Doc, operations []graphOperation,
) error {
	if hooks.CheckOperations != nil {
		if err := hooks.CheckOperations(app, authorization, doc, operations); err != nil {
			return err
		}
	}
	if hooks.BeforeApplyOperations == nil {
		return nil
	}
	return hooks.BeforeApplyOperations(app, authorization, doc, operations)
}
