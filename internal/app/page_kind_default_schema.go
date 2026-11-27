package app

// Every page has its kind, as its reads have: a page written without one —
// a workspace's first page, an import's root pages, the row and template
// pages a database's triggers make — is a page. A trigger gives it that
// kind as it is written, or saved again, without one, so every page writer
// stays as it is; the pages written without one since kinds arrived take it
// now.

const (
	pageKindDefaultVersion = 322
	pageKindDefaultName    = "native_page_kind_default"
)
