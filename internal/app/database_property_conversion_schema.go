package app

// A property type conversion records the source type and settings, converts
// the property's cells in bounded turns, and keeps each original cell for the
// recoverable-trash period so the conversion can be undone.

const (
	databasePropertyConversionPending   = "pending"
	databasePropertyConversionCompleted = "completed"
	databasePropertyConversionRestored  = "restored"
)
