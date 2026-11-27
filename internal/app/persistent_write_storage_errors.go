package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

type persistentWriteStorageOperation string

const (
	persistentWriteWorkspaceSettings      persistentWriteStorageOperation = "workspace-settings"
	persistentWriteWorkspaceCreate        persistentWriteStorageOperation = "workspace-create"
	persistentWriteWorkspaceMemberAdd     persistentWriteStorageOperation = "workspace-member-add"
	persistentWriteWorkspaceMemberRevoke  persistentWriteStorageOperation = "workspace-member-revoke"
	persistentWriteWorkspaceOwnerTransfer persistentWriteStorageOperation = "workspace-owner-transfer"
	persistentWriteAccountProfile         persistentWriteStorageOperation = "account-profile"
	persistentWriteWorkspaceDelete        persistentWriteStorageOperation = "workspace-delete"
	persistentWriteDatabaseAutomation     persistentWriteStorageOperation = "database-automation"
	persistentWriteDatabaseToolbarConfig  persistentWriteStorageOperation = "database-toolbar-config"
	persistentWriteInteractiveAutomation  persistentWriteStorageOperation = "interactive-automation"
	persistentWriteComment                persistentWriteStorageOperation = "comment"
	persistentReadComment                 persistentWriteStorageOperation = "comment-read"
	persistentReadNotificationInbox       persistentWriteStorageOperation = "notification-inbox-read"
	persistentWriteNotificationSync       persistentWriteStorageOperation = "notification-sync"
	persistentWriteNotificationRead       persistentWriteStorageOperation = "notification-read"
	persistentWriteNotificationArchive    persistentWriteStorageOperation = "notification-archive"
	persistentWriteNotificationState      persistentWriteStorageOperation = "notification-state"
	persistentReadWorkspaceSearch         persistentWriteStorageOperation = "workspace-search"
	persistentWriteWorkspaceSearchRebuild persistentWriteStorageOperation = "workspace-search-rebuild"
	persistentWritePageDuplication        persistentWriteStorageOperation = "page-duplication"
	persistentWriteBlockMove              persistentWriteStorageOperation = "block-move"
	persistentPageAnalytics               persistentWriteStorageOperation = "page-analytics"
	persistentPageAnalyticsRetention      persistentWriteStorageOperation = "page-analytics-retention"

	persistentWriteStorageUnavailableCauseCode = "PERSISTENT_WRITE_STORAGE_UNAVAILABLE"
	persistentWriteDatabaseBusyCauseCode       = "PERSISTENT_WRITE_DATABASE_BUSY"
)

type persistentWriteStorageErrorSpec struct {
	Operation         string
	StorageCode       string
	StorageMessage    string
	ContentionCode    string
	ContentionMessage string
}

type persistentWriteStorageSelection struct {
	Operation string
	Code      string
	Message   string
	CauseCode string
}

func persistentWriteStorageAPIError(
	event *core.RequestEvent,
	err error,
	operation persistentWriteStorageOperation,
	stage string,
) (error, bool) {
	selection, ok := persistentWriteStorageErrorSelection(err, operation)
	if !ok {
		return nil, false
	}
	return writeAPIError(
		event, http.StatusServiceUnavailable, selection.Code, selection.Message,
		selection.Operation, stage, selection.CauseCode, err, diagnosticSourcePosition(1),
	), true
}

func persistentWriteStorageErrorSelection(
	err error,
	operation persistentWriteStorageOperation,
) (persistentWriteStorageSelection, bool) {
	class := sqliteOperationalErrorClass(err)
	if class == sqliteOperationalNone {
		return persistentWriteStorageSelection{}, false
	}
	spec, ok := persistentWriteStorageErrorSpecFor(operation)
	if !ok {
		return persistentWriteStorageSelection{}, false
	}
	selection := persistentWriteStorageSelection{
		Operation: spec.Operation, Code: spec.StorageCode,
		Message: spec.StorageMessage, CauseCode: persistentWriteStorageUnavailableCauseCode,
	}
	if class == sqliteOperationalBusy {
		selection.Code = spec.ContentionCode
		selection.Message = spec.ContentionMessage
		selection.CauseCode = persistentWriteDatabaseBusyCauseCode
	}
	return selection, true
}

func persistentWriteStorageErrorSpecFor(
	operation persistentWriteStorageOperation,
) (persistentWriteStorageErrorSpec, bool) {
	switch operation {
	case persistentWriteWorkspaceCreate:
		return persistentWriteStorageErrorSpec{
			Operation:         "workspace.create",
			StorageCode:       errorCodeWorkspaceCreateStorageUnavailable,
			StorageMessage:    "Retry the same workspace creation after server storage recovers.",
			ContentionCode:    errorCodeWorkspaceCreateDatabaseBusy,
			ContentionMessage: "Retry the same workspace creation after the other server operation finishes.",
		}, true
	case persistentWriteWorkspaceSettings:
		return persistentWriteStorageErrorSpec{
			Operation:         "workspace-settings.update",
			StorageCode:       errorCodeWorkspaceSettingsStorageUnavailable,
			StorageMessage:    "Save the same workspace settings again after server storage recovers.",
			ContentionCode:    errorCodeWorkspaceSettingsDatabaseBusy,
			ContentionMessage: "Save the same workspace settings again after the other server operation finishes.",
		}, true
	case persistentWriteWorkspaceMemberAdd:
		return persistentWriteStorageErrorSpec{
			Operation:         "workspace-member.add",
			StorageCode:       errorCodeWorkspaceMemberAddStorageUnavailable,
			StorageMessage:    "Add the same workspace member again after server storage recovers.",
			ContentionCode:    errorCodeWorkspaceMemberAddDatabaseBusy,
			ContentionMessage: "Add the same workspace member again after the other server operation finishes.",
		}, true
	case persistentWriteWorkspaceMemberRevoke:
		return persistentWriteStorageErrorSpec{
			Operation:         "workspace.member.revoke",
			StorageCode:       errorCodeWorkspaceMemberMutationStorageUnavailable,
			StorageMessage:    "Retry the same workspace member removal after server storage recovers.",
			ContentionCode:    errorCodeWorkspaceMemberMutationDatabaseBusy,
			ContentionMessage: "Retry the same workspace member removal after the other server operation finishes.",
		}, true
	case persistentWriteWorkspaceOwnerTransfer:
		return persistentWriteStorageErrorSpec{
			Operation:         "workspace-owner.transfer",
			StorageCode:       errorCodeWorkspaceOwnerTransferStorageUnavailable,
			StorageMessage:    "Retry the same workspace ownership transfer after server storage recovers.",
			ContentionCode:    errorCodeWorkspaceOwnerTransferDatabaseBusy,
			ContentionMessage: "Retry the same workspace ownership transfer after the other server operation finishes.",
		}, true
	case persistentWriteAccountProfile:
		return persistentWriteStorageErrorSpec{
			Operation:         "account-profile.update",
			StorageCode:       errorCodeAccountProfileStorageUnavailable,
			StorageMessage:    "Save the same account profile again after server storage recovers.",
			ContentionCode:    errorCodeAccountProfileDatabaseBusy,
			ContentionMessage: "Save the same account profile again after the other server operation finishes.",
		}, true
	case persistentWriteWorkspaceDelete:
		return persistentWriteStorageErrorSpec{
			Operation:         "workspace-delete",
			StorageCode:       errorCodeWorkspaceDeleteStorageUnavailable,
			StorageMessage:    "Retry the same workspace deletion after server storage recovers.",
			ContentionCode:    errorCodeWorkspaceDeleteDatabaseBusy,
			ContentionMessage: "Retry the same workspace deletion after the other server operation finishes.",
		}, true
	case persistentWriteDatabaseAutomation:
		return persistentWriteStorageErrorSpec{
			Operation:         "database-automation.mutate",
			StorageCode:       errorCodeDatabaseAutomationStorageUnavailable,
			StorageMessage:    "Retry the same database automation change after server storage recovers.",
			ContentionCode:    errorCodeDatabaseAutomationDatabaseBusy,
			ContentionMessage: "Retry the same database automation change after the other server operation finishes.",
		}, true
	case persistentWriteDatabaseToolbarConfig:
		return persistentWriteStorageErrorSpec{
			Operation:         "database-toolbar-config.mutate",
			StorageCode:       errorCodeDatabaseToolbarConfigStorageUnavailable,
			StorageMessage:    "Retry the same database settings change after server storage recovers.",
			ContentionCode:    errorCodeDatabaseToolbarConfigDatabaseBusy,
			ContentionMessage: "Retry the same database settings change after the other server operation finishes.",
		}, true
	case persistentWriteInteractiveAutomation:
		return persistentWriteStorageErrorSpec{
			Operation:         "interactive-automation.execute",
			StorageCode:       errorCodeInteractiveAutomationStorageUnavailable,
			StorageMessage:    "Retry the same button automation run after server storage recovers.",
			ContentionCode:    errorCodeInteractiveAutomationDatabaseBusy,
			ContentionMessage: "Retry the same button automation run after the other server operation finishes.",
		}, true
	case persistentWriteComment:
		return persistentWriteStorageErrorSpec{
			Operation:         "comment.mutate",
			StorageCode:       errorCodeCommentStorageUnavailable,
			StorageMessage:    "Retry the same comment operation after server storage recovers.",
			ContentionCode:    errorCodeCommentDatabaseBusy,
			ContentionMessage: "Retry the same comment operation after the other server operation finishes.",
		}, true
	case persistentReadComment:
		return persistentWriteStorageErrorSpec{
			Operation:         "comment.read",
			StorageCode:       errorCodeCommentStorageUnavailable,
			StorageMessage:    "Retry the same comment query after server storage recovers.",
			ContentionCode:    errorCodeCommentDatabaseBusy,
			ContentionMessage: "Retry the same comment query after the other server operation finishes.",
		}, true
	case persistentReadNotificationInbox:
		return persistentWriteStorageErrorSpec{
			Operation:         "notification-inbox.read",
			StorageCode:       errorCodeCommentNotificationInboxStorageUnavailable,
			StorageMessage:    "Load the same notification list again after server storage recovers.",
			ContentionCode:    errorCodeCommentNotificationInboxDatabaseBusy,
			ContentionMessage: "Load the same notification list again after the other server operation finishes.",
		}, true
	case persistentWriteNotificationSync:
		return persistentWriteStorageErrorSpec{
			Operation:         "notification-sync.mutate",
			StorageCode:       errorCodeCommentNotificationSyncStorageUnavailable,
			StorageMessage:    "Retry the same notification sync after server storage recovers.",
			ContentionCode:    errorCodeCommentNotificationSyncDatabaseBusy,
			ContentionMessage: "Retry the same notification sync after the other server operation finishes.",
		}, true
	case persistentWriteNotificationRead:
		return persistentWriteStorageErrorSpec{
			Operation:         "notification-read.mutate",
			StorageCode:       errorCodeCommentNotificationReadStorageUnavailable,
			StorageMessage:    "Retry the same notification read operation after server storage recovers.",
			ContentionCode:    errorCodeCommentNotificationReadDatabaseBusy,
			ContentionMessage: "Retry the same notification read operation after the other server operation finishes.",
		}, true
	case persistentWriteNotificationArchive:
		return persistentWriteStorageErrorSpec{
			Operation:         "notification-archive.mutate",
			StorageCode:       errorCodeCommentNotificationArchiveStorageUnavailable,
			StorageMessage:    "Retry the same notification archive operation after server storage recovers.",
			ContentionCode:    errorCodeCommentNotificationArchiveDatabaseBusy,
			ContentionMessage: "Retry the same notification archive operation after the other server operation finishes.",
		}, true
	case persistentWriteNotificationState:
		return persistentWriteStorageErrorSpec{
			Operation:         "notification-state.mutate",
			StorageCode:       errorCodeCommentNotificationStateStorageUnavailable,
			StorageMessage:    "Retry the same notification change after server storage recovers.",
			ContentionCode:    errorCodeCommentNotificationStateDatabaseBusy,
			ContentionMessage: "Retry the same notification change after the other server operation finishes.",
		}, true
	case persistentReadWorkspaceSearch:
		return persistentWriteStorageErrorSpec{
			Operation:         "workspace-search.read",
			StorageCode:       errorCodeWorkspaceSearchStorageUnavailable,
			StorageMessage:    "Retry the same search after server storage recovers.",
			ContentionCode:    errorCodeWorkspaceSearchDatabaseBusy,
			ContentionMessage: "Retry the same search after the other server operation finishes.",
		}, true
	case persistentWriteWorkspaceSearchRebuild:
		return persistentWriteStorageErrorSpec{
			Operation:         "workspace-search.rebuild",
			StorageCode:       errorCodeWorkspaceSearchRebuildStorageUnavailable,
			StorageMessage:    "Retry the same search rebuild after server storage recovers.",
			ContentionCode:    errorCodeWorkspaceSearchRebuildDatabaseBusy,
			ContentionMessage: "Retry the same search rebuild after the other server operation finishes.",
		}, true
	case persistentWritePageDuplication:
		return persistentWriteStorageErrorSpec{
			Operation:         "page-duplication",
			StorageCode:       errorCodePageDuplicationStorageUnavailable,
			StorageMessage:    "Retry the same page duplication request after server storage recovers.",
			ContentionCode:    errorCodePageDuplicationDatabaseBusy,
			ContentionMessage: "Retry the same page duplication request after the other server operation finishes.",
		}, true
	case persistentWriteBlockMove:
		return persistentWriteStorageErrorSpec{
			Operation:         "block-move",
			StorageCode:       errorCodeBlockMoveStorageUnavailable,
			StorageMessage:    "Retry the same block move request after server storage recovers.",
			ContentionCode:    errorCodeBlockMoveDatabaseBusy,
			ContentionMessage: "Retry the same block move request after the other server operation finishes.",
		}, true
	case persistentPageAnalytics:
		return persistentWriteStorageErrorSpec{
			Operation:         "page_analytics.request",
			StorageCode:       errorCodePageAnalyticsStorageUnavailable,
			StorageMessage:    "Retry the same page analytics job after server storage recovers.",
			ContentionCode:    errorCodePageAnalyticsDatabaseBusy,
			ContentionMessage: "Retry the same page analytics job after the other server operation finishes.",
		}, true
	case persistentPageAnalyticsRetention:
		return persistentWriteStorageErrorSpec{
			Operation:         "page_analytics.retention",
			StorageCode:       errorCodePageAnalyticsRetentionStorageUnavailable,
			StorageMessage:    "The page analytics retention job will retry after server storage recovers.",
			ContentionCode:    errorCodePageAnalyticsRetentionDatabaseBusy,
			ContentionMessage: "The page analytics retention job will retry after the other server operation finishes.",
		}, true
	default:
		return persistentWriteStorageErrorSpec{}, false
	}
}
