package app

// Every kind of source a Notion import stages, the kinds whose children are
// still read from Notion after they are staged, and the kinds of parent a
// source or issue names. A later migration only adds kinds, so the
// validators of earlier migrations check stored sources against these
// current sets: an import that is running, or kept for its report, must not
// stop the server from starting again.
const (
	notionImportPendingSourceKindsSQL = `'page','row','paragraph','template','table','block'`
)
