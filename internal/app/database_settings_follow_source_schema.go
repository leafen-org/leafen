package app

// A database's sub-item, dependency and task settings, and its sprints, stay
// with its data source, as Notion keeps a database's settings when it moves:
// moving the database to another page takes them along. Databases moved
// before get their settings back on the page they sit on now.

const (
	databaseSettingsFollowSourceVersion = 316
	databaseSettingsFollowSourceName    = "native_database_settings_follow_source"
)
