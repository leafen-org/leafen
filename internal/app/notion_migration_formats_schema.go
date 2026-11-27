package app

// What a migration read in the person's signed-in Notion of what Notion's API
// doesn't give (notion_migration_formats_read.go): a row each, by its Notion
// id — a page's, row's or template's width, small text and font, a
// database's page layout, and a view's groups, wrap, the order people put its
// rows in, default template, conditions on deleted properties and a form's
// words — each rows' order once, for the views that have it (kind order, by
// what it holds) — and the reader's own row (kind reader, no id) holding the key the
// browser hands what it read in with, when it was read and matched, and which
// blocks have comments.

const notionMigrationFormatsTable = "leafen_notion_migration_formats"
