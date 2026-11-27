package app

// The codes of 404s that name what wasn't found, where no contract of the
// route's kind names one: a code says the kind, as LEAFEN_GROUP_NOT_FOUND a
// group, and the error's target gives its ID (api_error_details.go). A
// container's code (LEAFEN_WORKSPACE_NOT_FOUND) is for the container: what
// the person can't reach at all, which keeps from saying whether it is
// there; what is missing inside one they reach says its own kind
// (scripts/check-not-found-codes.mjs holds the two together).
const (
	errorCodeGroupNotFound           = "LEAFEN_GROUP_NOT_FOUND"
	errorCodeUserNotFound            = "LEAFEN_USER_NOT_FOUND"
	errorCodeWorkspaceMemberNotFound = "LEAFEN_WORKSPACE_MEMBER_NOT_FOUND"
	errorCodeInvitationNotFound      = "LEAFEN_INVITATION_NOT_FOUND"
	errorCodeOwnerTransferNotFound   = "LEAFEN_OWNER_TRANSFER_NOT_FOUND"
	errorCodeInviteLinkNotFound      = "LEAFEN_INVITE_LINK_NOT_FOUND"
	errorCodeBlockNotFound           = "LEAFEN_BLOCK_NOT_FOUND"
	errorCodeDatabaseNotFound        = "LEAFEN_DATABASE_NOT_FOUND"
	errorCodeTemplateNotFound        = "LEAFEN_TEMPLATE_NOT_FOUND"
	errorCodePropertyNotFound        = "LEAFEN_PROPERTY_NOT_FOUND"
	errorCodePageLinkNotFound        = "LEAFEN_PAGE_LINK_NOT_FOUND"
	errorCodeLoginWallNotFound       = "LEAFEN_LOGIN_WALL_NOT_FOUND"
	errorCodeArchiveUploadNotFound   = "LEAFEN_ARCHIVE_UPLOAD_NOT_FOUND"
	// A catalog the server doesn't trust, and a domain a workspace doesn't
	// allow, asked to be removed.
	errorCodeAppCatalogNotFound         = "LEAFEN_APP_CATALOG_NOT_FOUND"
	errorCodeAllowedEmailDomainNotFound = "LEAFEN_ALLOWED_EMAIL_DOMAIN_NOT_FOUND"
)

// ErrorCodeWorkspaceMemberNotFound is for the organization module's member
// routes (organization_kernel.go exports the rest).
const ErrorCodeWorkspaceMemberNotFound = errorCodeWorkspaceMemberNotFound
