package app

// The server's apps: each uploaded package kept by its fingerprint with its
// files, each installed app pointing at one package, who may use it in each
// tenancy (an organization, or the workspaces outside any), and what
// administrators did to it.

const (
	appPackagesTable     = "leafen_app_packages"
	appPackageFilesTable = "leafen_app_package_files"
	appInstallsTable     = "leafen_app_installs"
	appGrantsTable       = "leafen_app_grants"
	appAuditEventsTable  = "leafen_app_audit_events"
	appDelegationsTable  = "leafen_app_delegations"
)
