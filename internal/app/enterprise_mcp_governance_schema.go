package app

const (
	organizationMCPApprovedClientsTable = "leafen_organization_mcp_approved_clients"
)

func sqlStringLiteral(value string) string {
	return "'" + value + "'"
}
