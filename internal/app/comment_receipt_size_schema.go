package app

// A comment's receipt keeps the answer to the request that wrote it, its
// body too, so it holds as much as a comment does (comment-wire's
// bodyBytes) and the rest of the answer beside it: the table held 128 KiB,
// so a comment as long as Notion's API takes (200,000 characters of rich
// text, seen 2026-11-11) failed in the database.

// commentReceiptBytes is the most a receipt's answer holds: a comment's
// body, 2 MiB, and 64 KiB beside it.
const commentReceiptBytes = 2_162_688
