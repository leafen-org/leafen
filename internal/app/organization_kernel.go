package app

import (
	"context"
	"github.com/pocketbase/dbx"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"golang.org/x/text/cases"
)

// The core names the organization module uses, exported for it alone. The
// module is kept apart from the core and the core never imports it; this
// file is the whole of what it reaches in the core.

type (
	AccountDeletionWorkspace                   = accountDeletionWorkspace
	AccountEmailAddress                        = accountEmailAddress
	AccountEmailMutationInput                  = accountEmailMutationInput
	AccountEmailState                          = accountEmailState
	AdministratorWorkspaceOwnershipTransfer    = administratorWorkspaceOwnershipTransfer
	AuditRetentionPolicyResponse               = auditRetentionPolicyResponse
	AuditRetentionUpdateInput                  = auditRetentionUpdateInput
	AuthAppReturn                              = authAppReturn
	BrowserSessionMetadata                     = browserSessionMetadata
	ContentCustodianItem                       = contentCustodianItem
	DLPPolicyInput                             = dlpPolicyInput
	DLPPolicyResponse                          = dlpPolicyResponse
	DurableJobClock                            = durableJobClock
	DurableJobRunner                           = durableJobRunner
	DurableJobTimer                            = durableJobTimer
	LegalHoldCreateInput                       = legalHoldCreateInput
	LegalHoldDeletionTarget                    = legalHoldDeletionTarget
	LegalHoldRecordSQL                         = legalHoldRecordSQL
	LegalHoldListResponse                      = legalHoldListResponse
	LegalHoldReleaseInput                      = legalHoldReleaseInput
	LegalHoldResponse                          = legalHoldResponse
	LegalHoldScopeInput                        = legalHoldScopeInput
	LegalHoldScopeResponse                     = legalHoldScopeResponse
	MCPConnectionRevocationFilter              = mcpConnectionRevocationFilter
	MCPGovernanceApprovedClientInput           = mcpGovernanceApprovedClientInput
	MCPGovernanceApprovedClientResponse        = mcpGovernanceApprovedClientResponse
	MCPGovernanceContext                       = mcpGovernanceContext
	MCPGovernancePolicyInput                   = mcpGovernancePolicyInput
	MCPGovernancePolicyResponse                = mcpGovernancePolicyResponse
	MCPGovernanceRenameClientInput             = mcpGovernanceRenameClientInput
	MCPGovernanceWorkspace                     = mcpGovernanceWorkspace
	ManagedUserRules                           = managedUserRules
	NormalizedNotionAdminManagedUserSelector   = normalizedNotionAdminManagedUserSelector
	NotionAdminIdentity                        = notionAdminIdentity
	NotionPublicUserID                         = notionPublicUserID
	OrganizationAuditEvent                     = organizationAuditEvent
	OrganizationAuditEventListResponse         = organizationAuditEventListResponse
	OrganizationAuditEventResponse             = organizationAuditEventResponse
	OrganizationAuditExportContentResponse     = organizationAuditExportContentResponse
	OrganizationAuditExportCreateInput         = organizationAuditExportCreateInput
	OrganizationAuditExportResponse            = organizationAuditExportResponse
	OrganizationAuditFilter                    = organizationAuditFilter
	OrganizationAuthorization                  = organizationAuthorization
	OrganizationDiscoveryExportContentResponse = organizationDiscoveryExportContentResponse
	OrganizationDiscoveryExportCreateInput     = organizationDiscoveryExportCreateInput
	OrganizationDiscoveryExportListResponse    = organizationDiscoveryExportListResponse
	OrganizationDiscoveryExportResponse        = organizationDiscoveryExportResponse
	OrganizationDiscoveryFilter                = organizationDiscoveryFilter
	OrganizationSettingsDomain                 = organizationSettingsDomain
	OrganizationSettingsMutationInput          = organizationSettingsMutationInput
	PreparedPassword                           = preparedPassword
	RealDurableJobClock                        = realDurableJobClock
	SAMLConfigurationInput                     = samlConfigurationInput
	SAMLDomainInput                            = samlDomainInput
	SAMLDomainResponse                         = samlDomainResponse
	SAMLExchangeInput                          = samlExchangeInput
	SAMLManagementSnapshotResponse             = samlManagementSnapshotResponse
	SAMLProviderResponse                       = samlProviderResponse
	SAMLWorkspaceResponse                      = samlWorkspaceResponse
	SAMLServiceReturnInput                     = samlServiceReturnInput
	SCIMConfigurationInput                     = scimConfigurationInput
	SCIMConfigurationResponse                  = scimConfigurationResponse
	SCIMGroupMutationPayload                   = scimGroupMutationPayload
	SCIMGroupOperationMetadata                 = scimGroupOperationMetadata
	SCIMTokenCreateInput                       = scimTokenCreateInput
	SCIMTokenCreationResponse                  = scimTokenCreationResponse
	SCIMTokenMetadata                          = scimTokenMetadata
	SessionResponse                            = sessionResponse
	StoredAdministratorSettings                = storedAdministratorSettings
	StoredAuditRetentionEvent                  = storedAuditRetentionEvent
	StoredAuditRetentionSweep                  = storedAuditRetentionSweep
	StoredAuditRetentionWork                   = storedAuditRetentionWork
	StoredJob                                  = storedJob
	StoredOrganizationAuditExport              = storedOrganizationAuditExport
	StoredOrganizationDLPPolicy                = storedOrganizationDLPPolicy
	WorkspaceAuthorization                     = workspaceAuthorization
	WorkspaceRoomInvalidator                   = workspaceRoomInvalidator
	DatabasePropertyAccessException            = storedDatabasePropertyAccessException
)

const (
	APIRoot                                                 = apiRoot
	AccountDeletionBlockerNone                              = accountDeletionBlockerNone
	AccountLanguageKo                                       = accountLanguageKo
	AccountPasswordReasonReset                              = accountPasswordReasonReset
	AccountSecondaryEmailLimit                              = accountSecondaryEmailLimit
	AdministratorAccountLockNamespace                       = administratorAccountLockNamespace
	AdministratorAccountStatusActive                        = administratorAccountStatusActive
	AuditRetentionCron                                      = auditRetentionCron
	AuditRetentionEventsPerOrganization                     = auditRetentionEventsPerOrganization
	AuditRetentionEventsPerSlice                            = auditRetentionEventsPerSlice
	AuditRetentionGetPath                                   = auditRetentionGetPath
	AuditRetentionLogicalHourUTC                            = auditRetentionLogicalHourUTC
	AuditRetentionLogicalMinuteUTC                          = auditRetentionLogicalMinuteUTC
	AuditRetentionMaxBodyBytes                              = auditRetentionMaxBodyBytes
	AuditRetentionMinimumDays                               = auditRetentionMinimumDays
	AuditRetentionOrganizationsPerDiscovery                 = auditRetentionOrganizationsPerDiscovery
	AuditRetentionOrganizationsPerSlice                     = auditRetentionOrganizationsPerSlice
	AuditRetentionUpdatePath                                = auditRetentionUpdatePath
	AuthAppChallengeParam                                   = authAppChallengeParam
	AuthAppReturnParam                                      = authAppReturnParam
	AuthAuditMethodSAMLSignin                               = authAuditMethodSAMLSignin
	AuthAuditPhaseVerify                                    = authAuditPhaseVerify
	CipherSAMLProvider                                      = cipherSAMLProvider
	CommentTargetDatabaseRow                                = commentTargetDatabaseRow
	ContentCustodianWindowLimit                             = contentCustodianWindowLimit
	DLPContentScanBlock                                     = dlpContentScanBlock
	DLPMaxBodyBytes                                         = dlpMaxBodyBytes
	DLPMaxScannedStringRunes                                = dlpMaxScannedStringRunes
	DLPMaxScannedValues                                     = dlpMaxScannedValues
	DLPOutboundExports                                      = dlpOutboundExports
	DLPOutboundExternalSharing                              = dlpOutboundExternalSharing
	DLPOutboundFileDownloads                                = dlpOutboundFileDownloads
	DLPOutboundPublicSharing                                = dlpOutboundPublicSharing
	DefaultDurableJobMaxAttempts                            = defaultDurableJobMaxAttempts
	DurableJobReadyDelay                                    = durableJobReadyDelay
	DurableJobRunnerStoreKey                                = durableJobRunnerStoreKey
	DurableJobStatusCanceled                                = durableJobStatusCanceled
	DurableJobStatusCompleted                               = durableJobStatusCompleted
	DurableJobStatusFailed                                  = durableJobStatusFailed
	DurableJobStatusQueued                                  = durableJobStatusQueued
	ErrorCodeAuditRetentionInvalid                          = errorCodeAuditRetentionInvalid
	ErrorCodeAuditRetentionRunnerFailed                     = errorCodeAuditRetentionRunnerFailed
	ErrorCodeAuditRetentionUnavailable                      = errorCodeAuditRetentionUnavailable
	ErrorCodeDLPPolicyInvalid                               = errorCodeDLPPolicyInvalid
	ErrorCodeDLPUnavailable                                 = errorCodeDLPUnavailable
	ErrorCodeInvalidInput                                   = errorCodeInvalidInput
	ErrorCodeJobRunnerFailed                                = errorCodeJobRunnerFailed
	ErrorCodeLegalHoldInvalid                               = errorCodeLegalHoldInvalid
	ErrorCodeLegalHoldNotFound                              = errorCodeLegalHoldNotFound
	ErrorCodeLegalHoldPreventsPermanentDelete               = errorCodeLegalHoldPreventsPermanentDelete
	ErrorCodeLegalHoldUnavailable                           = errorCodeLegalHoldUnavailable
	ErrorCodeOrganizationAuditTooLarge                      = errorCodeOrganizationAuditTooLarge
	ErrorCodeOrganizationAuditUnavailable                   = errorCodeOrganizationAuditUnavailable
	ErrorCodeOrganizationAuthorityInvalid                   = errorCodeOrganizationAuthorityInvalid
	ErrorCodeOrganizationDiscoveryInvalid                   = errorCodeOrganizationDiscoveryInvalid
	ErrorCodeOrganizationDiscoveryNotFound                  = errorCodeOrganizationDiscoveryNotFound
	ErrorCodeOrganizationDiscoverySourceChanged             = errorCodeOrganizationDiscoverySourceChanged
	ErrorCodeOrganizationDiscoveryTooLarge                  = errorCodeOrganizationDiscoveryTooLarge
	ErrorCodeOrganizationDiscoveryUnavailable               = errorCodeOrganizationDiscoveryUnavailable
	ErrorCodeOrganizationNotFound                           = errorCodeOrganizationNotFound
	ErrorCodeOrganizationSettingsInvalid                    = errorCodeOrganizationSettingsInvalid
	ErrorCodeOrganizationSettingsVerificationFailed         = errorCodeOrganizationSettingsVerificationFailed
	ErrorCodePermissionDenied                               = errorCodePermissionDenied
	ErrorCodeSAMLAssertionCardinalityInvalid                = errorCodeSAMLAssertionCardinalityInvalid
	ErrorCodeSAMLAssertionReplayed                          = errorCodeSAMLAssertionReplayed
	ErrorCodeSAMLAttributesTooLarge                         = errorCodeSAMLAttributesTooLarge
	ErrorCodeSAMLCompletionConsumed                         = errorCodeSAMLCompletionConsumed
	ErrorCodeSAMLCompletionInvalid                          = errorCodeSAMLCompletionInvalid
	ErrorCodeSAMLConfigurationInvalid                       = errorCodeSAMLConfigurationInvalid
	ErrorCodeSAMLFieldsRequired                             = errorCodeSAMLFieldsRequired
	ErrorCodeSAMLLinkedWorkspaceInvalid                     = errorCodeSAMLLinkedWorkspaceInvalid
	ErrorCodeSAMLIssuerInvalid                              = errorCodeSAMLIssuerInvalid
	ErrorCodeSAMLLoginURLNotHTTPS                           = errorCodeSAMLLoginURLNotHTTPS
	ErrorCodeSAMLCertificateInvalid                         = errorCodeSAMLCertificateInvalid
	ErrorCodeSAMLEnforcementNeedsSSO                        = errorCodeSAMLEnforcementNeedsSSO
	ErrorCodeSAMLIdentityDenied                             = errorCodeSAMLIdentityDenied
	ErrorCodeSAMLAccountCreationOff                         = errorCodeSAMLAccountCreationOff
	ErrorCodeSAMLProviderNotFound                           = errorCodeSAMLProviderNotFound
	ErrorCodeSAMLRequestMismatch                            = errorCodeSAMLRequestMismatch
	ErrorCodeSAMLRequired                                   = errorCodeSAMLRequired
	ErrorCodeSAMLResponseInvalid                            = errorCodeSAMLResponseInvalid
	ErrorCodeSAMLResponseTooLarge                           = errorCodeSAMLResponseTooLarge
	ErrorCodeSAMLUnavailable                                = errorCodeSAMLUnavailable
	ErrorCodeSAMLWeakAlgorithm                              = errorCodeSAMLWeakAlgorithm
	ErrorCodeSAMLXMLTooComplex                              = errorCodeSAMLXMLTooComplex
	ErrorCodeSCIMConfigurationInvalid                       = errorCodeSCIMConfigurationInvalid
	ErrorCodeSCIMOperationConflict                          = errorCodeSCIMOperationConflict
	ErrorCodeSCIMOperationStateInvalid                      = errorCodeSCIMOperationStateInvalid
	ErrorCodeSCIMRequestFailed                              = errorCodeSCIMRequestFailed
	ErrorCodeSCIMTokenInvalid                               = errorCodeSCIMTokenInvalid
	ErrorCodeSCIMTokenNotFound                              = errorCodeSCIMTokenNotFound
	ErrorCodeSCIMUnavailable                                = errorCodeSCIMUnavailable
	ErrorCodeUnauthorized                                   = errorCodeUnauthorized
	ErrorCodeWorkspaceExportSourceChanged                   = errorCodeWorkspaceExportSourceChanged
	ErrorCodeWorkspaceMemberMutationFailed                  = errorCodeWorkspaceMemberMutationFailed
	ErrorCodeWorkspaceNotFound                              = errorCodeWorkspaceNotFound
	LegacyOrganizationAuditEventsPerExport                  = legacyOrganizationAuditEventsPerExport
	LegalHoldCreatePath                                     = legalHoldCreatePath
	LegalHoldListPath                                       = legalHoldListPath
	LegalHoldMaxBodyBytes                                   = legalHoldMaxBodyBytes
	LegalHoldMaxMutationIDRunes                             = legalHoldMaxMutationIDRunes
	LegalHoldMaxNameRunes                                   = legalHoldMaxNameRunes
	LegalHoldMaxPageSize                                    = legalHoldMaxPageSize
	LegalHoldMaxReasonRunes                                 = legalHoldMaxReasonRunes
	LegalHoldMaxScopeIDRunes                                = legalHoldMaxScopeIDRunes
	LegalHoldReleasePath                                    = legalHoldReleasePath
	LegalHoldScopeAll                                       = legalHoldScopeAll
	LegalHoldScopeCustodian                                 = legalHoldScopeCustodian
	LegalHoldScopePage                                      = legalHoldScopePage
	LegalHoldScopeWorkspace                                 = legalHoldScopeWorkspace
	LegalHoldStatusActive                                   = legalHoldStatusActive
	LegalHoldStatusReleased                                 = legalHoldStatusReleased
	MCPGovernanceApproveClientPath                          = mcpGovernanceApproveClientPath
	MCPGovernanceMaxBodyBytes                               = mcpGovernanceMaxBodyBytes
	MCPGovernanceMaxClientIDRunes                           = mcpGovernanceMaxClientIDRunes
	MCPGovernanceMaxClientNameRunes                         = mcpGovernanceMaxClientNameRunes
	MCPGovernanceReadPolicyPath                             = mcpGovernanceReadPolicyPath
	MCPGovernanceRemoveClientPath                           = mcpGovernanceRemoveClientPath
	MCPGovernanceRenameClientPath                           = mcpGovernanceRenameClientPath
	MCPGovernanceUpdatePolicyPath                           = mcpGovernanceUpdatePolicyPath
	ManagedUserPolicyHoursMillis                            = managedUserPolicyHoursMillis
	MaxAuthNameLength                                       = maxAuthNameLength
	MaxGraphIdentifierLength                                = maxGraphIdentifierLength
	NotificationEmailCleanupLimit                           = notificationEmailCleanupLimit
	NotificationEmailRetention                              = notificationEmailRetention
	NotificationEmailTitleRunes                             = notificationEmailTitleRunes
	NotionAdminAPILegalHoldsPath                            = notionAdminAPILegalHoldsPath
	NotionAdminAPIMCPConnectionsPath                        = notionAdminAPIMCPConnectionsPath
	NotionAdminAPIManagedUserSessionRevokePath              = notionAdminAPIManagedUserSessionRevokePath
	NotionAdminAPISpacesPath                                = notionAdminAPISpacesPath
	NotionAdminCapabilityLegalHoldExport                    = notionAdminCapabilityLegalHoldExport
	NotionAdminCapabilityLegalHoldRead                      = notionAdminCapabilityLegalHoldRead
	NotionAdminCapabilityLegalHoldWrite                     = notionAdminCapabilityLegalHoldWrite
	NotionAdminCapabilityLegalHoldWriteHighImpact           = notionAdminCapabilityLegalHoldWriteHighImpact
	NotionAdminCapabilityMCPClientConnectionRead            = notionAdminCapabilityMCPClientConnectionRead
	NotionAdminCapabilityMCPClientConnectionWriteHighImpact = notionAdminCapabilityMCPClientConnectionWriteHighImpact
	NotionAdminCapabilityManagedUserSessionWrite            = notionAdminCapabilityManagedUserSessionWrite
	NotionAdminCapabilityWorkflowsRead                      = notionAdminCapabilityWorkflowsRead
	NotionAdminCapabilityWorkflowsWrite                     = notionAdminCapabilityWorkflowsWrite
	NotionAdminErrorForbidden                               = notionAdminErrorForbidden
	NotionAdminErrorInternal                                = notionAdminErrorInternal
	NotionAdminErrorNotFound                                = notionAdminErrorNotFound
	NotionAdminErrorUnauthorized                            = notionAdminErrorUnauthorized
	NotionAdminErrorValidation                              = notionAdminErrorValidation
	NotionAdminForbiddenMessage                             = notionAdminForbiddenMessage
	NotionAdminInternalErrorMessage                         = notionAdminInternalErrorMessage
	NotionAdminLegalHoldMaxDescriptionRunes                 = notionAdminLegalHoldMaxDescriptionRunes
	NotionAdminLegalHoldMaxIconRunes                        = notionAdminLegalHoldMaxIconRunes
	NotionAdminLegalHoldMetadataVersion                     = notionAdminLegalHoldMetadataVersion
	NotionAdminNotFoundMessage                              = notionAdminNotFoundMessage
	NotionAdminUnauthorizedMessage                          = notionAdminUnauthorizedMessage
	NotionErrorObjectNotFound                               = notionErrorObjectNotFound
	NotionErrorRestrictedResource                           = notionErrorRestrictedResource
	NotionUserIdentityAlphabet                              = notionUserIdentityAlphabet
	NotionUserIdentityLength                                = notionUserIdentityLength
	OrganizationAuditEventsTable                            = organizationAuditEventsTable
	OrganizationAuditExportArtifactFormat                   = organizationAuditExportArtifactFormat
	OrganizationAuditExportChunkMaxBytes                    = organizationAuditExportChunkMaxBytes
	OrganizationAuditExportCreatePath                       = organizationAuditExportCreatePath
	OrganizationAuditExportDownloadChunkWindow              = organizationAuditExportDownloadChunkWindow
	OrganizationAuditExportDownloadPath                     = organizationAuditExportDownloadPath
	OrganizationAuditExportEventsPerTurn                    = organizationAuditExportEventsPerTurn
	OrganizationAuditExportFormatCSV                        = organizationAuditExportFormatCSV
	OrganizationAuditExportFormatJSON                       = organizationAuditExportFormatJSON
	OrganizationAuditExportFormatJSONL                      = organizationAuditExportFormatJSONL
	OrganizationAuditExportGetPath                          = organizationAuditExportGetPath
	OrganizationAuditExportListPath                         = organizationAuditExportListPath
	OrganizationAuditExportStatusCompleted                  = organizationAuditExportStatusCompleted
	OrganizationAuditExportStatusFailed                     = organizationAuditExportStatusFailed
	OrganizationAuditExportStatusPending                    = organizationAuditExportStatusPending
	OrganizationAuditExportStatusRunning                    = organizationAuditExportStatusRunning
	OrganizationAuditMaxBodyBytes                           = organizationAuditMaxBodyBytes
	OrganizationDiscoveryChunkMaxBytes                      = organizationDiscoveryChunkMaxBytes
	OrganizationDiscoveryCustodianMaxItems                  = organizationDiscoveryCustodianMaxItems
	OrganizationDiscoveryDownloadChunkWindow                = organizationDiscoveryDownloadChunkWindow
	OrganizationDiscoveryExportCreatePath                   = organizationDiscoveryExportCreatePath
	OrganizationDiscoveryExportDownloadPath                 = organizationDiscoveryExportDownloadPath
	OrganizationDiscoveryExportFormatJSON                   = organizationDiscoveryExportFormatJSON
	OrganizationDiscoveryExportFormatJSONL                  = organizationDiscoveryExportFormatJSONL
	OrganizationDiscoveryExportGetPath                      = organizationDiscoveryExportGetPath
	OrganizationDiscoveryExportListPath                     = organizationDiscoveryExportListPath
	OrganizationDiscoveryExportPhaseCapture                 = organizationDiscoveryExportPhaseCapture
	OrganizationDiscoveryExportPhaseCompleted               = organizationDiscoveryExportPhaseCompleted
	OrganizationDiscoveryExportPhaseFailed                  = organizationDiscoveryExportPhaseFailed
	OrganizationDiscoveryExportPhasePublish                 = organizationDiscoveryExportPhasePublish
	OrganizationDiscoveryExportStatusCompleted              = organizationDiscoveryExportStatusCompleted
	OrganizationDiscoveryExportStatusFailed                 = organizationDiscoveryExportStatusFailed
	OrganizationDiscoveryExportStatusPending                = organizationDiscoveryExportStatusPending
	OrganizationDiscoveryExportStatusRunning                = organizationDiscoveryExportStatusRunning
	OrganizationDiscoveryExportsPerPage                     = organizationDiscoveryExportsPerPage
	OrganizationDiscoveryIdentifierMaxRunes                 = organizationDiscoveryIdentifierMaxRunes
	OrganizationDiscoveryItemsPerTurn                       = organizationDiscoveryItemsPerTurn
	OrganizationDiscoveryMaximumBytes                       = organizationDiscoveryMaximumBytes
	OrganizationDiscoveryMaximumItems                       = organizationDiscoveryMaximumItems
	OrganizationDiscoveryQueryMaxRunes                      = organizationDiscoveryQueryMaxRunes
	OrganizationDiscoveryWorkspaceMaxItems                  = organizationDiscoveryWorkspaceMaxItems
	OrganizationDomainsTable                                = organizationDomainsTable
	OrganizationLegalHoldMutationsTable                     = organizationLegalHoldMutationsTable
	OrganizationLegalHoldsTable                             = organizationLegalHoldsTable
	OrganizationMCPApprovedClientsTable                     = organizationMCPApprovedClientsTable
	OrganizationMemberStatusActive                          = organizationMemberStatusActive
	OrganizationMemberStatusDeactivated                     = organizationMemberStatusDeactivated
	OrganizationMembershipsCollection                       = organizationMembershipsCollection
	OrganizationRoleAdmin                                   = organizationRoleAdmin
	OrganizationRoleBillingAdmin                            = organizationRoleBillingAdmin
	OrganizationRoleGuest                                   = organizationRoleGuest
	OrganizationRoleMember                                  = organizationRoleMember
	OrganizationRoleOwner                                   = organizationRoleOwner
	OrganizationRoleSecurityAdmin                           = organizationRoleSecurityAdmin
	OrganizationSettingsActionDomainAdd                     = organizationSettingsActionDomainAdd
	OrganizationSettingsActionDomainRemove                  = organizationSettingsActionDomainRemove
	OrganizationSettingsActionDomainVerify                  = organizationSettingsActionDomainVerify
	OrganizationSettingsDomainPageSize                      = organizationSettingsDomainPageSize
	OrganizationsCollection                                 = organizationsCollection
	PageInteractionCreated                                  = pageInteractionCreated
	PageInteractionEdited                                   = pageInteractionEdited
	PageInteractionViewed                                   = pageInteractionViewed
	PagesCollection                                         = pagesCollection
	PermissionOrganizationActive                            = permissionOrganizationActive
	PermissionOrganizationAuditLog                          = permissionOrganizationAuditLog
	PermissionOrganizationContentSearch                     = permissionOrganizationContentSearch
	PermissionOrganizationIdentityAdmin                     = permissionOrganizationIdentityAdmin
	PermissionOrganizationIntegrationsAdmin                 = permissionOrganizationIntegrationsAdmin
	PermissionOrganizationLegalHolds                        = permissionOrganizationLegalHolds
	PermissionOrganizationOwner                             = permissionOrganizationOwner
	PermissionOrganizationSecurityAdmin                     = permissionOrganizationSecurityAdmin
	PermissionWorkspaceRead                                 = permissionWorkspaceRead
	PermissionWorkspaceOwner                                = permissionWorkspaceOwner
	PublicMutationPrincipalEmail                            = publicMutationPrincipalEmail
	PublicMutationPrincipalUser                             = publicMutationPrincipalUser
	SAMLAssertionsTable                                     = samlAssertionsTable
	SAMLAuthenticationMethodSAML                            = samlAuthenticationMethodSAML
	SAMLCompletionTTLSeconds                                = samlCompletionTTLSeconds
	SAMLCompletionsTable                                    = samlCompletionsTable
	SAMLDomainPending                                       = samlDomainPending
	SAMLDomainVerified                                      = samlDomainVerified
	SAMLEnforcementAllMembers                               = samlEnforcementAllMembers
	SAMLEnforcementOptional                                 = samlEnforcementOptional
	SAMLAccountCreationAutomatic                            = samlAccountCreationAutomatic
	SAMLAccountCreationOff                                  = samlAccountCreationOff
	SAMLEnforcementVerifiedDomains                          = samlEnforcementVerifiedDomains
	SAMLManagementSnapshotPath                              = samlManagementSnapshotPath
	SAMLMaxAttributeTotalBytes                              = samlMaxAttributeTotalBytes
	SAMLMaxAttributes                                       = samlMaxAttributes
	SAMLMaxEncodedResponseBytes                             = samlMaxEncodedResponseBytes
	SAMLMaxRelayStateBytes                                  = samlMaxRelayStateBytes
	SAMLMaxXMLResponseBytes                                 = samlMaxXMLResponseBytes
	SAMLMaxXMLTokens                                        = samlMaxXMLTokens
	SAMLRequestTTLSeconds                                   = samlRequestTTLSeconds
	SAMLRequestsTable                                       = samlRequestsTable
	SCIMDeprovisionDeactivate                               = scimDeprovisionDeactivate
	SCIMGroupsTable                                         = scimGroupsTable
	SCIMListAccessAllFilterKey                              = scimListAccessAllFilterKey
	SCIMListAccessResourceTypeGroup                         = scimListAccessResourceTypeGroup
	SCIMListAccessResourceTypeUser                          = scimListAccessResourceTypeUser
	SCIMMaxBodyBytes                                        = scimMaxBodyBytes
	SCIMMaxPageSize                                         = scimMaxPageSize
	SCIMMaxPatchOperations                                  = scimMaxPatchOperations
	SCIMMaxTokenLabelRunes                                  = scimMaxTokenLabelRunes
	SCIMOperationBatchSize                                  = scimOperationBatchSize
	SCIMOperationCursorHeader                               = scimOperationCursorHeader
	SCIMOperationIDHeader                                   = scimOperationIDHeader
	SCIMOperationPhaseApply                                 = scimOperationPhaseApply
	SCIMOperationPhaseCleanup                               = scimOperationPhaseCleanup
	SCIMOperationRecoveryDiscard                            = scimOperationRecoveryDiscard
	SCIMOperationStatusCompleted                            = scimOperationStatusCompleted
	SCIMOperationStatusHeader                               = scimOperationStatusHeader
	SCIMOperationStatusPending                              = scimOperationStatusPending
	SCIMOperationTotalHeader                                = scimOperationTotalHeader
	SCIMOperationsTable                                     = scimOperationsTable
	SCIMProvisioningModeV2                                  = scimProvisioningModeV2
	SCIMTokensTable                                         = scimTokensTable
	SMTPDeliveryAccepted                                    = smtpDeliveryAccepted
	SMTPDeliveryUncertain                                   = smtpDeliveryUncertain
	WorkspaceMembershipRevocationLockNamespace              = workspaceMembershipRevocationLockNamespace
	WorkspaceRoleMember                                     = workspaceRoleMember
	WorkspaceRoomsStoreKey                                  = workspaceRoomsStoreKey
	WorkspacesCollection                                    = workspacesCollection
	YgoBlockTextRootPrefix                                  = ygoBlockTextRootPrefix
)

var (
	APIError                                          = apiError
	APIErrorWithCause                                 = apiErrorWithCause
	AccountDeletionPreview                            = accountDeletionPreview
	AccountEmailAPIError                              = accountEmailAPIError
	AccountEmailSnapshot                              = accountEmailSnapshot
	AccountEmailsChanged                              = accountEmailsChanged
	AccountHasTwoStepVerification                     = accountHasTwoStepVerification
	AccountSessionsCarrySecondFactor                  = accountSessionsCarrySecondFactor
	AskBeforeActing                                   = askBeforeActing
	AuthProviderSecondFactor                          = authProviderSecondFactor
	AcquireAuditRetentionLease                        = acquireAuditRetentionLease
	AcquireNotionAdminTokenLifecycleLock              = acquireNotionAdminTokenLifecycleLock
	AcquireProductOperationLock                       = acquireProductOperationLock
	ActivateSCIMListResource                          = activateSCIMListResource
	AddVerifiedAccountEmail                           = addVerifiedAccountEmail
	AdmitAccountDeletion                              = admitAccountDeletion
	AdmitAccountDeletionBy                            = admitAccountDeletionBy
	AdmitAccountWorkspaceDeletions                    = admitAccountWorkspaceDeletions
	AdmitVerifiedEmailInvitations                     = admitVerifiedEmailInvitations
	ClaimUnprovenAddress                              = claimUnprovenAddress
	AppendIdentityBits                                = appendIdentityBits
	AssertNoActiveLegalHoldForPermanentDelete         = assertNoActiveLegalHoldForPermanentDelete
	AuditAccountEmailChange                           = auditAccountEmailChange
	AuthAppReturnDestination                          = authAppReturnDestination
	AuthAppVerifierMatches                            = authAppVerifierMatches
	AuthProviderExternalIdentityID                    = authProviderExternalIdentityID
	AuthStorageAPIError                               = authStorageAPIError
	AuthenticateNotionAdminToken                      = authenticateNotionAdminToken
	AuthenticatedUser                                 = authenticatedUser
	AuthorizeNotionAdminAPI                           = authorizeNotionAdminAPI
	AuthorizeNotionAdminPublicAPI                     = authorizeNotionAdminPublicAPI
	AuthorizeOrganization                             = authorizeOrganization
	AuthorizeOrganizationAudit                        = authorizeOrganizationAudit
	AuthorizeWorkspace                                = authorizeWorkspace
	BearerToken                                       = bearerToken
	BoolInt                                           = boolInt
	BrowserSessionMetadataFromEvent                   = browserSessionMetadataFromEvent
	BumpOrganizationPolicy                            = bumpOrganizationPolicy
	BumpOrganizationPolicyContext                     = bumpOrganizationPolicyContext
	CompleteAuditRetentionSweepIfDrained              = completeAuditRetentionSweepIfDrained
	ConfiguredPublicOrigin                            = configuredPublicOrigin
	CurrentAccountEmailMutation                       = currentAccountEmailMutation
	DLPPolicyMatchesInput                             = dlpPolicyMatchesInput
	DLPStorageAPIError                                = dlpStorageAPIError
	DecodeNotionAdminLegalHoldScopeCursor             = decodeNotionAdminLegalHoldScopeCursor
	DecryptAdministratorSetting                       = decryptAdministratorSetting
	DeleteAuditRetentionEvents                        = deleteAuditRetentionEvents
	DiagnosticOperation                               = diagnosticOperation
	DiagnosticSourcePosition                          = diagnosticSourcePosition
	DiscoverAuditRetentionOrganizations               = discoverAuditRetentionOrganizations
	EmailDomain                                       = emailDomain
	EncodeExportContentHasher                         = encodeExportContentHasher
	EncodeNotionAdminLegalHoldScopeCursor             = encodeNotionAdminLegalHoldScopeCursor
	EncryptAdministratorSetting                       = encryptAdministratorSetting
	EnsureOrganizationPolicyState                     = ensureOrganizationPolicyState
	ErrAccountEmailChallenge                          = errAccountEmailChallenge
	ErrAccountEmailLimit                              = errAccountEmailLimit
	ErrAccountEmailManaged                            = errAccountEmailManaged
	ErrAccountEmailMissing                            = errAccountEmailMissing
	ErrAccountEmailPrimary                            = errAccountEmailPrimary
	ErrAuditRetentionConflict                         = errAuditRetentionConflict
	ErrAuditRetentionInvalid                          = errAuditRetentionInvalid
	ErrAuditRetentionPolicyInvalid                    = errAuditRetentionPolicyInvalid
	ErrAuditRetentionUnavailable                      = errAuditRetentionUnavailable
	ErrDLPPolicyBlocked                               = errDLPPolicyBlocked
	ErrDLPPolicyInvalid                               = errDLPPolicyInvalid
	ErrDLPPolicyUnavailable                           = errDLPPolicyUnavailable
	ErrExternalWorkspacesDisabled                     = errExternalWorkspacesDisabled
	ErrLegalHoldPreventsPermanentDelete               = errLegalHoldPreventsPermanentDelete
	ErrLegalHoldUnavailable                           = errLegalHoldUnavailable
	ErrMCPClientNotApproved                           = errMCPClientNotApproved
	ErrMCPGovernanceInvalid                           = errMCPGovernanceInvalid
	ErrMCPGovernanceUnavailable                       = errMCPGovernanceUnavailable
	ErrNotionAdminManagedUserSessionNotFound          = errNotionAdminManagedUserSessionNotFound
	ErrNotionAdminManagedUserSessionUnavailable       = errNotionAdminManagedUserSessionUnavailable
	ErrNotionAdminTokenUnauthorized                   = errNotionAdminTokenUnauthorized
	ErrOrganizationAuditConflict                      = errOrganizationAuditConflict
	ErrOrganizationAuditInvalid                       = errOrganizationAuditInvalid
	ErrOrganizationAuditNotFound                      = errOrganizationAuditNotFound
	ErrOrganizationAuditTooLarge                      = errOrganizationAuditTooLarge
	ErrOrganizationAuditUnavailable                   = errOrganizationAuditUnavailable
	ErrOrganizationSettingsInvalid                    = errOrganizationSettingsInvalid
	ErrOrganizationSharingSecurityBlocked             = errOrganizationSharingSecurityBlocked
	ErrRegistrationAccountExists                      = errRegistrationAccountExists
	ErrSAMLConfigurationInvalid                       = errSAMLConfigurationInvalid
	ErrSAMLRequired                                   = errSAMLRequired
	ErrDeviceSignInRestricted                         = errDeviceSignInRestricted
	ErrWorkspaceCreationRestricted                    = errWorkspaceCreationRestricted
	ErrWorkspaceRestrictedMemberTarget                = errWorkspaceRestrictedMemberTarget
	ErrDatabasePropertyAccessInvalid                  = errDatabasePropertyAccessInvalid
	ErrDatabasePropertyAccessNotFound                 = errDatabasePropertyAccessNotFound
	ErrYgoMaterializedBatchLimit                      = errYgoMaterializedBatchLimit
	FindAccountByEmail                                = findAccountByEmail
	FindOrganizationAuditExportByMutation             = findOrganizationAuditExportByMutation
	FormatNotionUserUUID                              = formatNotionUserUUID
	GuestMFARequired                                  = guestMFARequired
	HashAuthProviderToken                             = hashAuthProviderToken
	HashNotionAdminTokenRequest                       = hashNotionAdminTokenRequest
	IdentityBitsValue                                 = identityBitsValue
	InsertOrganizationAuditExport                     = insertOrganizationAuditExport
	InsertSCIMOperationMemberRange                    = insertSCIMOperationMemberRange
	IsAdministrator                                   = isAdministrator
	IsPermissionDenied                                = isPermissionDenied
	IssueAuthenticatedBrowserSessionInTransaction     = issueAuthenticatedBrowserSessionInTransaction
	JSONArrayExportWireFragment                       = jsonArrayExportWireFragment
	JoinSQL                                           = joinSQL
	ListOrganizationAuditEvents                       = listOrganizationAuditEvents
	ListOrganizationAuditExportsRoute                 = listOrganizationAuditExportsRoute
	LoadAdministratorAccountState                     = loadAdministratorAccountState
	LoadAdministratorSettings                         = loadAdministratorSettings
	LoadDueAuditRetentionWork                         = loadDueAuditRetentionWork
	LoadEnforcedSharingSecurityPolicy                 = loadEnforcedSharingSecurityPolicy
	LoadExpiredAuditRetentionEvents                   = loadExpiredAuditRetentionEvents
	LoadLegalHoldWorkspaceOrganization                = loadLegalHoldWorkspaceOrganization
	LoadMaterializedYgoDocumentsBounded               = loadMaterializedYgoDocumentsBounded
	LoadOrCreateAuditRetentionSweep                   = loadOrCreateAuditRetentionSweep
	LoadOrganizationAuditExport                       = loadOrganizationAuditExport
	LoadOrganizationAuditRetentionPolicy              = loadOrganizationAuditRetentionPolicy
	AuditRetentionCutoff                              = auditRetentionCutoff
	LoadOrganizationAuditRetentionPolicyContext       = loadOrganizationAuditRetentionPolicyContext
	LoadStoredJob                                     = loadStoredJob
	LoadStoredOrganizationAuditExport                 = loadStoredOrganizationAuditExport
	LogAPIDiagnostic                                  = logAPIDiagnostic
	LogDurableJobDiagnostic                           = logDurableJobDiagnostic
	LogNotificationEmailFailure                       = logNotificationEmailFailure
	MCPGovernanceManagementError                      = mcpGovernanceManagementError
	ManagedAccountDomainSQL                           = managedAccountDomainSQL
	MutateAccountProfileAuthority                     = mutateAccountProfileAuthority
	NewAuthProviderToken                              = newAuthProviderToken
	RehearseBeforeAsking                              = rehearseBeforeAsking
	ReturnAuthProviderFailures                        = returnAuthProviderFailures
	NewDiagnosticReference                            = newDiagnosticReference
	NewLegalHoldFailure                               = newLegalHoldFailure
	NextAuditRetentionWakeAt                          = nextAuditRetentionWakeAt
	NormalizeDLPPolicyInput                           = normalizeDLPPolicyInput
	NormalizeEmailIdentity                            = normalizeEmailIdentity
	NormalizeMCPGovernanceClientID                    = normalizeMCPGovernanceClientID
	NormalizeOrganizationAuditExportInput             = normalizeOrganizationAuditExportInput
	NormalizeOrganizationDomain                       = normalizeOrganizationDomain
	NormalizePageShareEmail                           = normalizePageShareEmail
	NotificationEmailConfigured                       = notificationEmailConfigured
	NotificationEmailLine                             = notificationEmailLine
	NotifyAdministratorAccountCleanupRunner           = notifyAdministratorAccountCleanupRunner
	NotifyNotificationEmailRunnerAfterCommit          = notifyNotificationEmailRunnerAfterCommit
	NotionAdminAPIError                               = notionAdminAPIError
	NotionAdminInvalid                                = notionAdminInvalid
	NotionAdminValidationReason                       = notionAdminValidationReason
	NotionAdminRefusalReason                          = notionAdminRefusalReason
	NotionAdminScopeChanged                           = notionAdminScopeChangedReason
	NotionAdminLegalHoldScopeQuery                    = notionAdminLegalHoldScopeQuery
	NotionAdminPublicAPIError                         = notionAdminPublicAPIError
	NotionAdminTokenHash                              = notionAdminTokenHash
	NotionCompatibleGroupID                           = notionCompatibleGroupID
	NotionCompatibleUserID                            = notionCompatibleUserID
	NotionCompatibleWorkspaceID                       = notionCompatibleWorkspaceID
	NotionUserIdentityFixedBit                        = notionUserIdentityFixedBit
	OrganizationAuditError                            = organizationAuditError
	OrganizationAuditExportFromStored                 = organizationAuditExportFromStored
	OrganizationAuditID                               = organizationAuditID
	OrganizationSettingsTime                          = organizationSettingsTime
	ParseAuthAppReturn                                = parseAuthAppReturn
	ParseNotionUserUUID                               = parseNotionUserUUID
	PermissionDenied                                  = permissionDenied
	OrganizationSettingsChanges                       = organizationSettingsChanges
	ProjectNotionUserIdentity                         = projectNotionUserIdentity
	PublicServerOrigin                                = publicServerOrigin
	RecheckCollaborationAccessAfterCommit             = recheckCollaborationAccessAfterCommit
	RecordAuthAttemptInTransactionSuccessWithMetadata = recordAuthAttemptInTransactionSuccessWithMetadata
	RecordManagedAccountDeletionAudit                 = recordManagedAccountDeletionAudit
	RecordOrganizationAudit                           = recordOrganizationAudit
	RecordOrganizationAuditAtContext                  = recordOrganizationAuditAtContext
	RecordOrganizationAuditEvent                      = recordOrganizationAuditEvent
	RecordOrganizationAuditEventContext               = recordOrganizationAuditEventContext
	ReleaseAuditRetentionLease                        = releaseAuditRetentionLease
	ResolveNotionAdminManagedUser                     = resolveNotionAdminManagedUser
	ResolveNotionCompatibleGroupID                    = resolveNotionCompatibleGroupID
	ResolveNotionCompatibleUserID                     = resolveNotionCompatibleUserID
	ResolveNotionCompatibleWorkspaceID                = resolveNotionCompatibleWorkspaceID
	ResolveNotionUserIdentityFilter                   = resolveNotionUserIdentityFilter
	RestoreExportContentHasher                        = restoreExportContentHasher
	RevokeAllBrowserSessions                          = revokeAllBrowserSessions
	RevokeAllBrowserSessionsCount                     = revokeAllBrowserSessionsCount
	RevokeMCPConnectionsInTransaction                 = revokeMCPConnectionsInTransaction
	RevokeWorkspaceMembershipInTransaction            = revokeWorkspaceMembershipInTransaction
	SCIMListAccessExternalFilterKey                   = scimListAccessExternalFilterKey
	SCIMOperationMetadata                             = scimOperationMetadata
	SafeDiagnosticToken                               = safeDiagnosticToken
	SendConfiguredSMTP                                = sendConfiguredSMTP
	SerializeOrganizationAuditEvents                  = serializeOrganizationAuditEvents
	ServerTextLanguage                                = serverTextLanguage
	SetMustChangePasswordInTransaction                = setMustChangePasswordInTransaction
	SetPreparedPassword                               = preparedPassword.set
	SetWorkspaceMemberRestricted                      = setWorkspaceMemberRestricted
	LoadDatabasePagePropertyAccess                    = loadDatabasePagePropertyAccess
	LoadDatabasePropertyAccessRule                    = loadDatabasePropertyAccessRule
	SetDatabasePropertyAccessRule                     = setDatabasePropertyAccessRule
	PreviewDatabasePropertyAccess                     = previewDatabasePropertyAccess
	SharingSecurityEmailMember                        = sharingSecurityEmailMember
	StoreOrganizationAuditRetentionPolicy             = storeOrganizationAuditRetentionPolicy
	SyncSCIMGroupListAccessInTransaction              = syncSCIMGroupListAccessInTransaction
	TransferWorkspaceOwnershipInTransaction           = transferWorkspaceOwnershipInTransaction
	UniqueSortedSCIMMemberIDs                         = uniqueSortedSCIMMemberIDs
	UnknownPassword                                   = unknownPassword
	UpdateAuditRetentionWork                          = updateAuditRetentionWork
	UserKeyAsksWhereItActs                            = userKeyAsksWhereItActs
	VerifiedAccountEmails                             = verifiedAccountEmails
	WorkspaceExportRevision                           = workspaceExportRevision
	WorkspaceMembershipRevocationRunner               = workspaceMembershipRevocationRunner
	WorkspaceRestrictedMembers                        = workspaceRestrictedMembers
	WriteAPIError                                     = writeAPIError
)

func DurableJobClockNewTimer(r durableJobClock, p0 time.Duration) durableJobTimer {
	return r.newTimer(p0)
}

func DurableJobClockNow(r durableJobClock) time.Time { return r.now() }

func DurableJobTimerChannelValue(r durableJobTimer) <-chan time.Time { return r.channelValue() }

func DurableJobTimerStop(r durableJobTimer) bool { return r.stop() }

func (r *durableJobRunner) App() core.App { return r.app }

func (r *durableJobRunner) Notify() { r.notify() }

func (r notionAdminIdentity) WorkspaceIDs() map[string]struct{} { return r.workspaceIDs }

func AuthMFANow() func() time.Time { return authMFANow }

func (r *durableJobRunner) AdvanceWorkspaceMembershipRevocation(p0 context.Context, p1 string) (bool, error) {
	return r.advanceWorkspaceMembershipRevocation(p0, p1)
}

func (r *durableJobRunner) Clock() durableJobClock { return r.clock }

func (r *durableJobRunner) Rooms() workspaceRoomInvalidator { return r.rooms }

func DLPFold() cases.Caser { return dlpFold }

func (r storedOrganizationDLPPolicy) Response() (dlpPolicyResponse, error) { return r.response() }

func (r notionAdminIdentity) LegalHoldIDs() map[string]struct{} { return r.legalHoldIDs }

// JoinOrganizationWorkspace makes someone new to an organization a member of
// one of its workspaces, as its SAML provider's linked workspace.
func JoinOrganizationWorkspace(app core.App, workspaceID, organizationID, userID string, metadata map[string]any) error {
	_, err := addWorkspaceMember(app, workspaceID, organizationID, userID, userID, time.Now().UTC().UnixMilli(), metadata)
	return err
}

// OrganizationWorkspaceSummaries lists an organization's workspaces that
// aren't being deleted, by name, with their owner and members counted as the
// workspace menu counts them: the table of what its SAML configuration covers.
func OrganizationWorkspaceSummaries(app core.App, organizationID string) ([]samlWorkspaceResponse, error) {
	rows := []struct {
		ID          string `db:"id"`
		Name        string `db:"name"`
		Created     string `db:"created"`
		MemberCount int    `db:"member_count"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[workspace]].[[id]], [[workspace]].[[name]], [[workspace]].[[created]],
			` + workspaceMemberCountSQL("[[workspace]].[[id]]") + ` AS [[member_count]]
		FROM {{leafen_workspaces}} [[workspace]]
		WHERE [[workspace]].[[organization]] = {:organization} AND [[workspace]].[[deletion_job_id]] = ''
		ORDER BY lower([[workspace]].[[name]]), [[workspace]].[[id]]`).Bind(dbx.Params{
		"organization": organizationID,
	}).All(&rows); err != nil {
		return nil, err
	}
	summaries := make([]samlWorkspaceResponse, 0, len(rows))
	for _, row := range rows {
		createdAt := row.Created
		if created, err := types.ParseDateTime(row.Created); err == nil && !created.IsZero() {
			createdAt = created.Time().UTC().Format(time.RFC3339)
		}
		summaries = append(summaries, samlWorkspaceResponse{
			ID: row.ID, Name: row.Name, MemberCount: row.MemberCount, CreatedAt: createdAt,
		})
	}
	return summaries, nil
}

// OrganizationWorkspaceOpen says whether a workspace belongs to an organization and isn't being deleted.
func OrganizationWorkspaceOpen(app core.App, workspaceID, organizationID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspaces}}
		WHERE [[id]]={:id} AND [[organization]]={:organization} AND [[deletion_job_id]]=''`).Bind(dbx.Params{
		"id": workspaceID, "organization": organizationID,
	}).Row(&count)
	return count > 0, err
}
