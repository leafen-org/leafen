package app

import "time"

// A Notion import job's payload — where its reading and making are, carried
// from turn to turn — and the records its report and its files are read into.

type notionImportJobPayload struct {
	ConnectionID                string   `json:"connectionId"`
	RootIDs                     []string `json:"rootIds"`
	RootKinds                   []string `json:"rootKinds,omitempty"`
	AuthorityOwnerID            string   `json:"authorityOwnerId"`
	Phase                       string   `json:"phase"`
	RootIndex                   int      `json:"rootIndex"`
	RootStaged                  bool     `json:"rootStaged"`
	RootChildPosition           int      `json:"rootChildPosition"`
	ProviderCursor              string   `json:"providerCursor"`
	PendingDatabaseIDs          []string `json:"pendingDatabaseIds"`
	ActiveDatabaseID            string   `json:"activeDatabaseId"`
	DatabasePropertyCursor      string   `json:"databasePropertyCursor"`
	DatabasePropertyFingerprint string   `json:"databasePropertyFingerprint"`
	DatabasePropertiesComplete  bool     `json:"databasePropertiesComplete"`
	DatabaseCursor              string   `json:"databaseCursor"`
	DatabaseRowPosition         int      `json:"databaseRowPosition"`
	// The minute the last row staged was made in and how many rows came in
	// it, for the created times of the rows Notion made in one minute.
	DatabaseRowMinutes notionImportRowMinutes `json:"databaseRowMinutes"`
	// A database Notion refuses in the older API version, one with several
	// data sources, is read through its first data source.
	DatabaseDataSourceID string `json:"databaseDataSourceId,omitempty"`
	// Its other data sources follow in turn, each staged under the database
	// by its key, with its own properties, rows and archived rows.
	DatabaseOtherSources []notionProviderOtherDataSource `json:"databaseOtherSources,omitempty"`
	DatabaseSourceKey    string                          `json:"databaseSourceKey,omitempty"`
	DatabaseSourceName   string                          `json:"databaseSourceName,omitempty"`
	DatabaseSourceIndex  int                             `json:"databaseSourceIndex,omitzero"`
	// After a database's rows, its archived rows are read from each of its
	// data sources in turn.
	DatabaseArchivedPhase         bool     `json:"databaseArchivedPhase,omitzero"`
	DatabaseArchivedSourcesLoaded bool     `json:"databaseArchivedSourcesLoaded,omitzero"`
	DatabaseArchivedSourceIDs     []string `json:"databaseArchivedSourceIds,omitempty"`
	// What the import calls each data source a linked database shows, once
	// Notion has said; "" for one it won't say.
	LinkedSourceKeys map[string]string `json:"linkedSourceKeys,omitempty"`
	// And by that key, its name, which the report names one outside the
	// import by (notion_import_linked_views.go).
	LinkedSourceNames map[string]string `json:"linkedSourceNames,omitempty"`
	// Then its views are listed and each is read, in its tab order.
	DatabaseSourceIDs       []string `json:"databaseSourceIds,omitempty"`
	DatabaseViewPhase       bool     `json:"databaseViewPhase,omitzero"`
	DatabaseViewsListed     bool     `json:"databaseViewsListed,omitzero"`
	DatabaseViewCursor      string   `json:"databaseViewCursor,omitempty"`
	DatabaseViewIDs         []string `json:"databaseViewIds,omitempty"`
	DatabaseViewPosition    int      `json:"databaseViewPosition,omitzero"`
	DatabaseViewPrimary     bool     `json:"databaseViewPrimary,omitzero"`
	DatabaseViewManualOrder bool     `json:"databaseViewManualOrder,omitzero"`
	// The data sources whose first tab the import has staged.
	DatabaseViewSourcesSeen []string `json:"databaseViewSourcesSeen,omitempty"`
	PendingFileIDs          []string `json:"pendingFileIds"`
	// Then the pages hanging unmentioned on its blocks (notion_import_unmentioned_pages.go).
	notionImportUnmentionedSweep
	// The second apply pass, over what merged databases hold whose target
	// was made after them (notion_migration_merge.go).
	ApplyingMerges bool `json:"applyingMerges,omitzero"`
	// What the making left out — what Leafen refused, and what it held —
	// by source (notion_import_left_out.go).
	LeftOut []string `json:"leftOut,omitempty"`
	// What a stopped run left out of the windows it had passed, made on its
	// resume in a pass of its own up to where it stopped, and the properties
	// made after rows of theirs, which those rows take (notion_import_late.go).
	Late           []string `json:"late,omitempty"`
	LateCursor     int64    `json:"lateCursor,omitzero"`
	LateUntil      int64    `json:"lateUntil,omitzero"`
	LateProperties []string `json:"lateProperties,omitempty"`
	// Once every source is applied, relations and rollups are linked, then
	// the pages are published.
	Linking           bool   `json:"linking,omitzero"`
	LinkCursor        int64  `json:"linkCursor,omitzero"`
	LinkingViews      bool   `json:"linkingViews,omitzero"`
	Publishing        bool   `json:"publishing"`
	PublicationCursor string `json:"publicationCursor"`
	// A design-first migration's run carries out the plan revision it was
	// started with (notion_migration_run.go).
	MigrationID           string `json:"migrationId,omitempty"`
	MigrationPlanRevision int    `json:"migrationPlanRevision,omitzero"`
	// A run after the first (notion_migration_run_again.go).
	notionMigrationRunAgain
	// A direct import's reading, looked at while it goes, and the time it
	// stood with Leafen off (notion_import_reading_clock.go).
	notionImportReadingClock
	// After publishing, the plan's property changes, one database a turn
	// (notion_migration_restructure.go).
	Restructuring    bool `json:"restructuring,omitzero"`
	RestructureIndex int  `json:"restructureIndex,omitzero"`
	// Last, each page and then each row takes its Notion times and people
	// (notion_import_page_stamps.go, notion_import_row_stamps.go).
	StampingRows    bool   `json:"stampingRows,omitzero"`
	PageStampCursor string `json:"pageStampCursor,omitempty"`
	StampCursor     string `json:"stampCursor,omitempty"`
	// A row the report named became a template in that last step, so the
	// report written before it is written again at the end
	// (repointNotionMigrationIssuesToTemplate).
	ReportNamedTemplateRow bool `json:"reportNamedTemplateRow,omitzero"`
	// The last page whose comments came, how many pages and rows were asked
	// of all there are to ask, and the people mapping's continuation.
	CommentCursor      string `json:"commentCursor,omitempty"`
	CommentPagesDone   int    `json:"commentPagesDone,omitzero"`
	CommentPagesTotal  int    `json:"commentPagesTotal,omitzero"`
	PeopleContinuation string `json:"peopleContinuation,omitempty"`
	// Once the pages' comments came, the blocks' (notion_migration_comment_discussions.go).
	notionMigrationRunOrder
	CommentBlocks      bool   `json:"commentBlocks,omitzero"`
	CommentBlockCursor string `json:"commentBlockCursor,omitempty"`
	CommentBlocksDone  int    `json:"commentBlocksDone,omitzero"`
	CommentBlocksTotal int    `json:"commentBlocksTotal,omitzero"`
	// When Notion asked the connection to wait partway through a turn's
	// comment asks: the turn keeps what it got before, and the next turn
	// asks on from there then (notion_migration_comment_asks.go). Not kept
	// with the job.
	commentsAskAfter time.Time
	// Where a run again's walk over the ledger for the blocks the source
	// deleted is, and whether the ledger has what the run made, to keep how
	// the run left it (notion_migration_removed.go).
	RemoveCursor   string `json:"removeCursor,omitempty"`
	LedgerRecorded bool   `json:"ledgerRecorded,omitzero"`
	// Where a run again's moves of the blocks the source moved are, and
	// where the ledger's record of where the run left its blocks is
	// (notion_migration_block_place.go).
	ReorderCursor string `json:"reorderCursor,omitempty"`
	PlaceCursor   string `json:"placeCursor,omitempty"`
}

type storedNotionImportReport struct {
	Pages        int `db:"pages"`
	Blocks       int `db:"blocks"`
	Databases    int `db:"databases"`
	Rows         int `db:"rows"`
	FilesCopied  int `db:"files_copied"`
	FilesOmitted int `db:"files_omitted"`
}

type storedNotionFileTask struct {
	SourceID  string `db:"source_id"`
	Kind      string `db:"kind"`
	Blueprint bool   `db:"blueprint"`
	Name      string `db:"name"`
	URLCipher string `db:"url_cipher"`
	Status    string `db:"status"`
	FileID    string `db:"file_id"`
	ErrorCode string `db:"error_code"`
	Attempt   int    `db:"attempt"`
}
