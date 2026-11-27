package app

// The schema steps of the blueprints and the recommended structure, which
// they still run on a database that hasn't had them, before the step that
// drops their tables (2026-10-29: Leafen keeps neither). Nothing else uses
// them.

const (
	blueprintAdoptionNoticePrefix   = "blueprint-version:"
	blueprintAdoptionDraftMostBytes = 4 << 20
)

const (
	blueprintMaxBytes         = 16 << 20
	blueprintNoticePagePrefix = "blueprint:"
	blueprintNoticeTarget     = "/admin/settings?section=blueprints"
)
