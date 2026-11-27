package app

// A workspace archive's import keeps each imported database's settings until
// it publishes its pages; a job that goes away takes what it kept along.

const (
	workspaceImportArchiveTaskSettingsVersion = 317
	workspaceImportArchiveTaskSettingsName    = "native_archive_task_settings"
)
