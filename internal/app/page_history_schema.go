package app

// A checkpoint keeps the whole page, and a version is the checkpoint before
// it replayed with the changes since. One is looked for every
// pageHistoryCheckpointInterval versions and taken once the changes since
// the last weigh as much as it, once the page holds fewer than half its
// blocks, or after pageHistoryCheckpointSpacingMax versions whatever they
// weigh (pageHistoryCheckpointDue).
const (
	pageHistoryCheckpointInterval   = 64
	pageHistoryCheckpointSpacingMax = 4096
)

// Bounded snapshot rows are a derived, rebuildable projection; the durable
// checkpoint and event tables below remain the sole exported history authority.
