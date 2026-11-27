package app

// Each person's own state of a comment thread, kept on the thread's first
// comment: whether they turned off notifications of its replies, and
// whether they marked it unread. A row goes with its comment, and a row
// with neither is not kept.

const (
	commentThreadStatesTable = "leafen_comment_thread_states"
)
