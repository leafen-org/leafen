package app

import "encoding/json"

type workspaceImportArchiveStage struct {
	Source     storedWorkspaceImportSource
	Title      string
	TotalItems int
	Pages      int
	Losses     []workspaceBundleLoss
}

// workspaceImportArchivePages is the pages a bundle brings, its database pages
// with them: the size its import's card says, as the export that made it said
// it (workspace_export.go), where its total counts the steps of putting it in.
func workspaceImportArchivePages(descriptor workspaceExportArchiveDescriptor) int {
	return descriptor.Counts["pages"] + descriptor.Counts["databaseRows"]
}

type workspaceImportArchiveUpload struct {
	StorageKey string
	SHA256     string
	Bytes      int64
}

type workspaceImportArchiveFile struct {
	SourceID    string `json:"sourceId"`
	TargetID    string `json:"targetId"`
	Offset      int64  `json:"offset"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Scope       string `json:"scope"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	ChainSHA256 string `json:"chainSha256"`
}

type workspaceImportStagedPage struct {
	ID           string  `json:"id"`
	TeamspaceID  string  `json:"teamspaceId"`
	ParentID     string  `json:"parentId"`
	Position     float64 `json:"position"`
	BodyPosition *int    `json:"bodyPosition,omitempty"`
	IsLocked     bool    `json:"isLocked"`
	Title        string  `json:"title"`
	// TitleSpans are its title's mentions, links and formatting, their pages and people those here.
	TitleSpans             []graphTextSpan `json:"titleSpans,omitempty"`
	Content                string          `json:"content"`
	Icon                   string          `json:"icon"`
	IconType               string          `json:"iconType"`
	Cover                  string          `json:"cover"`
	CoverPosition          int             `json:"coverPosition"`
	Font                   string          `json:"font"`
	SmallText              bool            `json:"smallText"`
	FullWidth              bool            `json:"fullWidth"`
	BacklinksDisplay       string          `json:"backlinksDisplay"`
	PageCommentsDisplay    string          `json:"pageCommentsDisplay"`
	TableOfContentsDisplay string          `json:"tableOfContentsDisplay"`
	InlineCommentsDisplay  string          `json:"inlineCommentsDisplay"`
	Kind                   string          `json:"kind,omitempty"` // none from an archive made before kinds were kept
	// MovedDatabase is the imported database a full-page database's page
	// left as a signpost leads to.
	MovedDatabase string `json:"movedDatabase,omitempty"`
	// Where it hangs: the imported line or field, when the archive holds it
	// (workspace_archive_page_anchor.go).
	workspaceImportPageAnchor
	InTrash   bool    `json:"inTrash"`
	TrashedAt *string `json:"trashedAt,omitempty"`
	// ArchivedAt keeps when the page was archived; the importer becomes its
	// archiver, as it becomes the custodian of every imported page.
	ArchivedAt *string `json:"archivedAt,omitempty"`
	Root       bool    `json:"root"`
	HasYgo     bool    `json:"hasYgo"`
	// Teamspace is the archived Teamspace a root page lives in.
	Teamspace *workspaceImportStagedTeamspace `json:"teamspace,omitempty"`
	// Reactions are the page's own, put back once the page is.
	Reactions []workspaceImportArchiveReaction `json:"reactions,omitempty"`
	// CreatedBy is the account here a moved workspace's page was made by
	// (workspace_import_archive_people.go); without one it is the importer.
	CreatedBy string `json:"createdBy,omitempty"`
	// EditedAt is when the page was last edited where it was exported from,
	// which it keeps, so the sidebar lists the pages last edited first as
	// there; none from an archive without it, and the page is edited now.
	EditedAt string `json:"editedAt,omitempty"`
}

type workspaceImportStagedRowPresentation struct {
	RowID         string `json:"rowId"`
	Icon          string `json:"icon"`
	IconType      string `json:"iconType"`
	Cover         string `json:"cover,omitempty"`
	CoverPosition int    `json:"coverPosition,omitempty"`
	// Font, SmallText and FullWidth are the row page's style, as a page's.
	Font                   string `json:"font,omitempty"`
	SmallText              bool   `json:"smallText,omitempty"`
	FullWidth              bool   `json:"fullWidth,omitempty"`
	TableOfContentsDisplay string `json:"tableOfContentsDisplay,omitempty"`
	InlineCommentsDisplay  string `json:"inlineCommentsDisplay,omitempty"`
	// ArchivedAt keeps when the row page was archived; the importer becomes
	// its archiver, as for pages.
	ArchivedAt *string `json:"archivedAt,omitempty"`
	// TrashedAt keeps a trashed row in trash with the rows trashed with it.
	TrashedAt   *string `json:"trashedAt,omitempty"`
	TrashRootID string  `json:"trashRootId,omitempty"`
	// SubPageIDs are the row page's sub-pages, made under the page holding
	// its database before the row page is and moved under it once it is.
	SubPageIDs []string `json:"subPageIds,omitempty"`
	// Reactions are the row page's own, put back with its presentation.
	Reactions []workspaceImportArchiveReaction `json:"reactions,omitempty"`
}

type workspaceImportStagedComment struct {
	ID          string                           `json:"id"`
	PageID      string                           `json:"pageId"`
	TargetKind  string                           `json:"targetKind"`
	TargetID    string                           `json:"targetId"`
	BlockID     string                           `json:"blockId"`
	ParentID    string                           `json:"parentId"`
	Body        json.RawMessage                  `json:"body"`
	Resolved    bool                             `json:"resolved"`
	CreatedAt   string                           `json:"createdAt"`
	EditedAt    string                           `json:"editedAt"`
	Reactions   []workspaceImportArchiveReaction `json:"reactions,omitempty"`
	ThreadState *workspaceImportThreadState      `json:"threadState,omitempty"`
	// AuthorID is the account here a moved workspace's comment was written
	// by; without one it is the person importing.
	AuthorID string `json:"authorId,omitempty"`
}

type workspaceImportStagedTemplate struct {
	ID           string          `json:"id"`
	PageID       string          `json:"pageId"`
	DataSourceID string          `json:"dataSourceId"`
	Name         string          `json:"name"`
	Icon         string          `json:"icon"`
	Title        string          `json:"title"`
	Properties   json.RawMessage `json:"properties"`
	// PropertySpans is the formatting of its text values, by property.
	PropertySpans json.RawMessage `json:"propertySpans,omitempty"`
	Blocks        json.RawMessage `json:"blocks"`
	IsDefault     bool            `json:"isDefault"`
	Position      string          `json:"position"`
	CreatedAt     string          `json:"createdAt"`
}

type workspaceImportStagedCustomEmoji struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	FileID    string `json:"fileId"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Carried   bool   `json:"carried,omitzero"`
}

type workspaceImportArchiveEntity struct {
	Kind  string         `json:"kind"`
	Value map[string]any `json:"value"`
}

type archivePageValue struct {
	ID           string   `json:"id"`
	TeamspaceID  string   `json:"teamspaceId"`
	ParentID     string   `json:"parentId"`
	Position     *float64 `json:"position,omitempty"`
	BodyPosition *int     `json:"bodyPosition,omitempty"`
	IsLocked     *bool    `json:"isLocked,omitempty"`
	Title        string   `json:"title"`
	// TitleSpans are an ordinary page's title's (page_title_spans.go).
	TitleSpans             []graphTextSpan `json:"titleSpans,omitempty"`
	Content                string          `json:"content"`
	Icon                   string          `json:"icon"`
	IconType               string          `json:"iconType"`
	Cover                  string          `json:"cover"`
	CoverPosition          int             `json:"coverPosition"`
	Font                   *string         `json:"font,omitempty"`
	SmallText              *bool           `json:"smallText,omitempty"`
	FullWidth              *bool           `json:"fullWidth,omitempty"`
	BacklinksDisplay       *string         `json:"backlinksDisplay,omitempty"`
	PageCommentsDisplay    *string         `json:"pageCommentsDisplay,omitempty"`
	TableOfContentsDisplay *string         `json:"tableOfContentsDisplay,omitempty"`
	InlineCommentsDisplay  *string         `json:"inlineCommentsDisplay,omitempty"`
	Kind                   *string         `json:"kind,omitempty"`
	MovedDatabase          *string         `json:"movedDatabase,omitempty"`
	// AnchorBlockID is the line a sub-page made from its mention hangs on
	// (mention_anchored_pages.go), AnchorLost whether it is in the trash for
	// that line's sake and AnchorUnmentioned whether it hangs there
	// unmentioned (mention_anchored_unmentioned.go).
	AnchorBlockID     *string `json:"anchorBlockId,omitempty"`
	AnchorLost        *bool   `json:"anchorLost,omitempty"`
	AnchorUnmentioned *bool   `json:"anchorUnmentioned,omitempty"`
	InTrash           bool    `json:"inTrash"`
	TrashedAt         *string `json:"trashedAt,omitempty"`
	ArchivedAt        *string `json:"archivedAt,omitempty"`
	// Reactions are the page's own, as a feed post's.
	Reactions []workspaceImportArchiveReaction `json:"reactions,omitempty"`
	// CreatedBy is who made it on the Leafen server it was exported from.
	CreatedBy string `json:"createdBy,omitempty"`
	// UpdatedAt is when it was last edited there.
	UpdatedAt string `json:"updatedAt,omitempty"`
	// CustomEmoji is its workspace emoji icon's picture, in a page's archive
	// (workspace_archive_custom_emoji_icon.go).
	CustomEmoji *archiveCustomEmojiPicture `json:"customEmoji,omitempty"`
}

type archiveBlockValue struct {
	ID           string          `json:"id"`
	PageID       string          `json:"pageId"`
	ParentID     *string         `json:"parentId"`
	Position     string          `json:"position"`
	BodyPosition *int            `json:"bodyPosition,omitempty"`
	Type         string          `json:"type"`
	Content      json.RawMessage `json:"content"`
}

type archiveBlockTrashValue struct {
	RootBlockID   string                        `json:"rootBlockId"`
	PageID        string                        `json:"pageId"`
	ParentID      string                        `json:"parentId"`
	PredecessorID string                        `json:"predecessorId"`
	SuccessorID   string                        `json:"successorId"`
	Snapshot      recoverableBlockTrashSnapshot `json:"snapshot"`
	BlockCount    int                           `json:"blockCount"`
	DeletedAt     int64                         `json:"deletedAt"`
	ExpiresAt     int64                         `json:"expiresAt"`
}

type archiveDataSourceValue struct {
	ID                         string          `json:"id"`
	PageID                     string          `json:"pageId"`
	Title                      string          `json:"title"`
	TitleSpans                 []graphTextSpan `json:"titleSpans,omitempty"`
	Icon                       string          `json:"icon"`
	IconType                   string          `json:"iconType"`
	Description                string          `json:"description,omitempty"`
	DescriptionSpans           []graphTextSpan `json:"descriptionSpans,omitempty"`
	DescriptionHidden          bool            `json:"descriptionHidden,omitzero"`
	Cover                      string          `json:"cover,omitempty"`
	CoverPosition              *int            `json:"coverPosition,omitempty"`
	Wiki                       bool            `json:"wiki,omitempty"`
	WikiOwnerPropertyID        string          `json:"wikiOwnerPropertyId,omitempty"`
	WikiVerificationPropertyID string          `json:"wikiVerificationPropertyId,omitempty"`
	// TaskFeatures and DependencyBinding are the data source's sub-item,
	// dependency and task settings and its dependency binding.
	TaskFeatures      *archiveTaskFeatures      `json:"taskFeatures,omitempty"`
	DependencyBinding *archiveDependencyBinding `json:"dependencyBinding,omitempty"`
	// StarterIdentity is what it was made as (data_source_starter_identity.go).
	StarterIdentity *archiveDataSourceStarterIdentity `json:"starterIdentity,omitempty"`
	// CustomEmoji is its workspace emoji icon's picture, in a page's archive
	// (workspace_archive_custom_emoji_icon.go).
	CustomEmoji *archiveCustomEmojiPicture `json:"customEmoji,omitempty"`
}

type archiveTaskFeatures struct {
	Subitems     json.RawMessage `json:"subitems,omitempty"`
	Dependencies json.RawMessage `json:"dependencies,omitempty"`
	Tasks        json.RawMessage `json:"tasks,omitempty"`
}

type archiveDependencyBinding struct {
	PredecessorPropertyID string `json:"predecessorPropertyId"`
	SuccessorPropertyID   string `json:"successorPropertyId"`
	DataKey               string `json:"dataKey"`
}

type archiveDatabaseContainerValue struct {
	ID                string   `json:"id"`
	PageID            string   `json:"pageId"`
	BlockID           string   `json:"blockId"`
	ParentBlockID     *string  `json:"parentBlockId"`
	Position          string   `json:"position"`
	Kind              string   `json:"kind"`
	SourceContainerID *string  `json:"sourceContainerId"`
	DataSourceIDs     []string `json:"dataSourceIds"`
	ViewIDs           []string `json:"viewIds"`
}

type archiveViewValue struct {
	ID           string          `json:"id"`
	PageID       string          `json:"pageId"`
	DataSourceID string          `json:"dataSourceId"`
	ContainerID  string          `json:"containerId,omitempty"`
	Type         string          `json:"type"`
	Name         string          `json:"name"`
	Position     string          `json:"position"`
	Config       json.RawMessage `json:"config"`
}

type archivePropertyValue struct {
	ID           string          `json:"id"`
	PageID       string          `json:"pageId"`
	DataSourceID string          `json:"dataSourceId"`
	Type         string          `json:"type"`
	Name         string          `json:"name"`
	Position     string          `json:"position"`
	Config       json.RawMessage `json:"config"`
	DeletedAt    *string         `json:"deletedAt,omitempty"`
}

type archiveRowValue struct {
	ID                     string                   `json:"id"`
	PageID                 string                   `json:"pageId"`
	DataSourceID           string                   `json:"dataSourceId"`
	SubitemParentID        *string                  `json:"subitemParentId"`
	Position               string                   `json:"position"`
	Values                 map[string]graphRowValue `json:"values"`
	Icon                   string                   `json:"icon"`
	IconType               string                   `json:"iconType"`
	Cover                  string                   `json:"cover"`
	CoverPosition          int                      `json:"coverPosition"`
	Font                   *string                  `json:"font,omitempty"`
	SmallText              *bool                    `json:"smallText,omitempty"`
	FullWidth              *bool                    `json:"fullWidth,omitempty"`
	TableOfContentsDisplay *string                  `json:"tableOfContentsDisplay,omitempty"`
	InlineCommentsDisplay  *string                  `json:"inlineCommentsDisplay,omitempty"`
	ArchivedAt             *string                  `json:"archivedAt,omitempty"`
	TrashedAt              *string                  `json:"trashedAt,omitempty"`
	TrashRootID            *string                  `json:"trashRootId,omitempty"`
	// Reactions are the row page's own, as a feed post's.
	Reactions []workspaceImportArchiveReaction `json:"reactions,omitempty"`
	// CustomEmoji is its workspace emoji icon's picture, in a page's archive
	// (workspace_archive_custom_emoji_icon.go).
	CustomEmoji *archiveCustomEmojiPicture `json:"customEmoji,omitempty"`
}

type archiveRowPageBlockValue struct {
	ID        string          `json:"id"`
	PageID    string          `json:"pageId"`
	RowID     string          `json:"rowId"`
	ParentID  string          `json:"parentId"`
	Position  string          `json:"position"`
	Type      string          `json:"type"`
	Content   json.RawMessage `json:"content"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

type archiveRowBlockTrashBlock struct {
	ID        string          `json:"id"`
	ParentID  string          `json:"parentId"`
	Position  string          `json:"position"`
	Type      string          `json:"type"`
	Content   json.RawMessage `json:"content"`
	CreatedAt int64           `json:"created"`
	UpdatedAt int64           `json:"updated"`
}

type archiveRowBlockTrashSnapshot struct {
	Format int                         `json:"format"`
	RootID string                      `json:"rootId"`
	Blocks []archiveRowBlockTrashBlock `json:"blocks"`
}

type archiveRowBlockTrashValue struct {
	RootBlockID   string                       `json:"rootBlockId"`
	PageID        string                       `json:"pageId"`
	RowID         string                       `json:"rowId"`
	ParentID      string                       `json:"parentId"`
	PredecessorID string                       `json:"predecessorId"`
	SuccessorID   string                       `json:"successorId"`
	Snapshot      archiveRowBlockTrashSnapshot `json:"snapshot"`
	BlockCount    int                          `json:"blockCount"`
	DeletedAt     int64                        `json:"deletedAt"`
	ExpiresAt     int64                        `json:"expiresAt"`
}

type archiveYgoValue struct {
	PageID             string `json:"pageId"`
	FormatVersion      int    `json:"formatVersion"`
	StateHex           string `json:"stateHex"`
	StructuralStateHex string `json:"structuralStateHex"`
}

type archiveCommentValue struct {
	ID          string                           `json:"id"`
	PageID      string                           `json:"pageId"`
	TargetKind  string                           `json:"targetKind"`
	TargetID    string                           `json:"targetId"`
	BlockID     string                           `json:"blockId"`
	ParentID    string                           `json:"parentId"`
	Body        json.RawMessage                  `json:"body"`
	Resolved    bool                             `json:"resolved"`
	CreatedAt   string                           `json:"createdAt"`
	EditedAt    string                           `json:"editedAt"`
	Reactions   []workspaceImportArchiveReaction `json:"reactions,omitempty"`
	ThreadState *workspaceImportThreadState      `json:"threadState,omitempty"`
	// Author names who wrote it in another tool; its Leafen author is the
	// person importing.
	Author string `json:"author,omitempty"`
	// AuthorID is who wrote it on the Leafen server it was exported from.
	AuthorID string `json:"authorId,omitempty"`
}

// workspaceImportArchiveReaction is one person's emoji on a comment; the
// person is kept by ID, as a comment's mentions are.
type workspaceImportArchiveReaction struct {
	Emoji  string `json:"emoji"`
	UserID string `json:"userId"`
}

// workspaceImportThreadState is the exporting person's own state of a
// thread, which becomes the importing person's.
type workspaceImportThreadState struct {
	RepliesMuted bool `json:"repliesMuted"`
	Unread       bool `json:"unread"`
}

type archiveTemplateValue struct {
	ID           string          `json:"id"`
	PageID       string          `json:"pageId"`
	DataSourceID string          `json:"dataSourceId"`
	Name         string          `json:"name"`
	Icon         string          `json:"icon"`
	Title        string          `json:"title"`
	Properties   json.RawMessage `json:"properties"`
	// PropertySpans is absent from archives made before templates kept the
	// formatting of their text values.
	PropertySpans json.RawMessage `json:"propertySpans"`
	Blocks        json.RawMessage `json:"blocks"`
	IsDefault     bool            `json:"isDefault"`
	Position      string          `json:"position"`
	CreatedAt     string          `json:"createdAt"`
	// CustomEmoji is its workspace emoji icon's picture, in a page's archive
	// (workspace_archive_custom_emoji_icon.go).
	CustomEmoji *archiveCustomEmojiPicture `json:"customEmoji,omitempty"`
}

type archiveCustomEmojiValue struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	FileID    string `json:"fileId"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	// Carried: no emoji of the workspace's own (custom_emoji_carried.go).
	Carried bool `json:"carried,omitzero"`
}
