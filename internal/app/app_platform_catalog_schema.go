package app

// App catalogs an administrator trusts (docs/app-platform.md 16): each
// signed index as last read, the packages a closed network's bundle brought,
// which catalog a package came from, and why a revoked install stopped.

const (
	appCatalogsTable = "leafen_app_catalogs"
)
