package app

// How people come to apps (docs/app-platform.md 3): only administrators
// install them (the default), people ask and administrators decide, or
// people add the apps administrators allowed themselves; and whether apps
// nobody signed may be installed (yes, unless turned off).

// People's requests for apps, the apps administrators let people add
// themselves, and each person's consent to what an app they added may do.

const (
	appRequestsTable = "leafen_app_requests"
)
