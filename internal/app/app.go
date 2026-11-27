package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"leafen.org/leafen/web"
)

const apiRoot = "/api/leafen/v1"

// webAppPath is the route the app's own pages and build files are served at.
const webAppPath = "/{path...}"

// workspaceDataConnections is how many read connections the data database
// keeps, all staying open between bursts: a new connection reads Leafen's
// whole schema first, so a burst that opened many made everything wait —
// concurrent searches took turns with the pool's default fifteen idle, and
// hundreds of tabs reconnecting after a restart waited four seconds behind
// the connections opened under them — while SQLite here gains little from
// more connections at once (2026-10-14 measurements).
const workspaceDataConnections = 32

// New is the Leafen server, keeping its data where its command line says
// ("data" beside it unless --dir names another folder).
func New() *pocketbase.PocketBase {
	return NewWithDataDir("data")
}

// NewWithDataDir is the Leafen server keeping its data in the given folder
// unless its command line names another, as the packaged client runs it:
// the data folder is settled from the process's own arguments before any
// command runs.
func NewWithDataDir(dataDir string) *pocketbase.PocketBase {
	pb := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dataDir,
		HideStartBanner: true,
		// Idle ones still close after three minutes; a small server keeps
		// fewer (memory_plan.go).
		DataMaxOpenConns: dataReadConnections(),
		DataMaxIdleConns: dataReadConnections(),
		// Data connections keep their statements compiled (sqlite_statement_cache.go).
		DBConnect: pocketBaseDBConnect,
	})
	// What a call first makes and keeps writes as the server (server_own_app.go).
	keepServerOwnApp(pb)
	setupOpenMode := strings.TrimSpace(os.Getenv(setupOpenModeEnvironment))
	if setupOpenMode == "" {
		setupOpenMode = setupOpenModeBackground
	}
	pb.RootCmd.PersistentFlags().StringVar(
		&setupOpenMode,
		"setup-open",
		setupOpenMode,
		"first-run setup URL handling: background or print",
	)
	registerDataWork(pb)
	registerSQLiteLookaside(pb)
	registerWALCheckpoints(pb)
	registerWriteConnectionGate(pb)
	registerPendingRestore(pb)
	registerRetiredProductDataRefusal(pb)
	registerServerDataLock(pb)
	registerReleaseKeyCheck(pb)
	registerSQLitePlannerStatistics(pb)
	// The clock's jobs stop with the server (server_background.go).
	ownServerClock(pb)
	registerProductSchema(pb)
	registerSetupState(pb)
	// Bound after the schema so the statements the migrations run are traced.
	registerSQLTrace(pb)
	jobs := newDurableJobRunner(pb)
	pb.Store().Set(durableJobRunnerStoreKey, jobs)
	notionRetention := newNotionImportRetentionRunner(pb)
	pb.Store().Set(notionImportRetentionRunnerStoreKey, notionRetention)
	blockTrashRetention := newRecoverableBlockTrashRetentionRunner(pb)
	pb.Store().Set(recoverableBlockTrashRetentionRunnerStoreKey, blockTrashRetention)
	fileText := newFileTextRunner(pb)
	pb.Store().Set(fileTextRunnerStoreKey, fileText)
	authSessionCleanup := newAuthSessionCleanupRunner(pb)
	signInEnds := &signInEndsFollower{}
	readConnectionsKept := newReadConnectionsKeeper(pb)
	pb.Store().Set(authSessionCleanupRunnerStoreKey, authSessionCleanup)
	pageInteractionRetention := newPageInteractionRetentionRunner(pb)
	pb.Store().Set(pageInteractionRetentionRunnerKey, pageInteractionRetention)
	pageAnalyticsRetention := newPageAnalyticsRetentionRunner(pb)
	pb.Store().Set(pageAnalyticsRetentionRunnerStoreKey, pageAnalyticsRetention)
	notificationMaterialization := newCommentNotificationMaterializationRunner(pb)
	pb.Store().Set(commentNotificationMaterializationRunnerStoreKey, notificationMaterialization)
	notificationEmails := newNotificationEmailRunner(pb)
	pb.Store().Set(notificationEmailRunnerStoreKey, notificationEmails)
	webPush := newWebPushRunner(pb)
	pb.Store().Set(webPushRunnerStoreKey, webPush)
	slackNotifications := newSlackNotificationRunner(pb)
	pb.Store().Set(slackNotificationRunnerStoreKey, slackNotifications)
	eventWebhooks := newEventWebhookRunner(pb)
	pb.Store().Set(eventWebhookRunnerStoreKey, eventWebhooks)
	notificationDigests := newNotificationDigestRunner(pb)
	pb.Store().Set(notificationDigestRunnerStoreKey, notificationDigests)
	reminders := newReminderRunner(pb)
	pb.Store().Set(reminderRunnerStoreKey, reminders)
	peopleGuestRows := newPeopleGuestRowsRunner(pb)
	bootstrapVisibilityProjection := newBootstrapVisibilityProjectionRunner(pb)
	pb.Store().Set(bootstrapVisibilityProjectionRunnerStoreKey, bootstrapVisibilityProjection)
	publicShareGraphUpkeep := newPublicShareGraphUpkeepRunner(pb)
	pb.Store().Set(publicShareGraphUpkeepRunnerStoreKey, publicShareGraphUpkeep)
	wikiVerification := newWikiVerificationRunner(pb, newNativeWikiVerificationEmailProvider())
	pb.Store().Set(wikiVerificationRunnerStoreKey, wikiVerification)
	mcpCredentialRegistry := newMCPCredentialRegistryLifecycle(pb)
	pb.Store().Set(mcpCredentialRegistryStoreKey, mcpCredentialRegistry)
	mcpSSESessions := newMCPSSESessionRuntime(pb)
	mcpCredentialRegistry.bindSSESessionRuntime(mcpSSESessions)
	urlMetadata := newURLMetadataService(newURLMetadataFetcher())
	administratorMutations := newAdministratorMutationLifecycle()
	fileMaintenance := newProductFileMaintenanceRunner(pb)
	pb.Store().Set(productFileMaintenanceRunnerStoreKey, fileMaintenance)
	notionFileUploadImport := newNotionFileUploadImportRunner(pb)
	pb.Store().Set(notionFileUploadImportRunnerStoreKey, notionFileUploadImport)
	fileManualCleanup := newProductFileManualCleanupRunner(pb, fileMaintenance)
	pb.Store().Set(productFileManualCleanupRunnerStoreKey, fileManualCleanup)
	releaseManifest := newReleaseManifestRunner(pb)
	pb.Store().Set(releaseManifestRunnerStoreKey, releaseManifest)
	scheduledBackups := newScheduledBackupRunner(pb)
	pb.Store().Set(scheduledBackupRunnerStoreKey, scheduledBackups)
	replication := newReplicationRunner(pb)
	pb.Store().Set(replicationRunnerStoreKey, replication)
	registerReplication(pb, replication)
	registerDatabaseAutomationScheduleCron(pb, jobs)

	ygoPersistence := newPocketBaseYgoPersistence(pb)
	pb.Store().Set(ygoPersistenceStoreKey, ygoPersistence)
	databaseMutationDeletion := newDatabaseMutationDeletionRunner(pb, ygoPersistence)
	pb.Store().Set(databaseMutationDeletionRunnerStoreKey, databaseMutationDeletion)
	databasePropertyConversion := newDatabasePropertyConversionRunner(pb, ygoPersistence)
	pb.Store().Set(databasePropertyConversionRunnerStoreKey, databasePropertyConversion)
	databaseRelationPairs := newDatabaseRelationPairRunner(pb, ygoPersistence)
	pb.Store().Set(databaseRelationPairRunnerStoreKey, databaseRelationPairs)
	databaseUniqueIDs := newDatabaseUniqueIDRunner(pb, ygoPersistence)
	pb.Store().Set(databaseUniqueIDRunnerStoreKey, databaseUniqueIDs)
	peopleValueInvitees := newPeopleValueInviteeRunner(pb, ygoPersistence)
	pb.Store().Set(peopleValueInviteeRunnerStoreKey, peopleValueInvitees)
	databaseSprints := newDatabaseSprintRunner(pb, ygoPersistence)
	pb.Store().Set(databaseSprintRunnerStoreKey, databaseSprints)
	searchIndexer := newWorkspaceSearchIndexer(pb)
	pb.Store().Set(workspaceSearchIndexerStoreKey, searchIndexer)
	jobs.bindYgoPersistence(ygoPersistence)
	ygoPersistence.bindJobNotifier(jobs.notify)
	ygoCompaction := newYgoCompactionRunner(pb, ygoPersistence)
	collaboration := newCollaborationServer(ygoPersistence)
	roomAuthorization := newYgoRoomAuthorization(pb, collaboration)
	pb.Store().Set(workspaceRoomsStoreKey, workspaceRoomInvalidator(roomAuthorization))
	pb.Store().Set(ygoRoomAuthorizationStoreKey, roomAuthorization)
	jobs.bindRoomInvalidator(roomAuthorization)
	blockTrashRetention.bindRoomInvalidator(roomAuthorization)
	pageDeletions := newPageDeletionRunner(pb, workspaceRoomInvalidator(roomAuthorization))
	roomAuthorization.bindPersistenceSettlement(ygoPersistence.settlement)
	collaboration.Authorize = roomAuthorization.Authorize
	collaboration.OnFirstPeer = roomAuthorization.OnFirstPeer
	collaboration.OnLastPeer = roomAuthorization.OnLastPeer
	collaboration.OnPeerUpdate = roomAuthorization.OnPeerUpdate

	// The app platform's background work, stopped before the database closes.
	appPlatformShutdown := func(context.Context) error { return nil }
	pb.OnServe().BindFunc(func(event *core.ServeEvent) error {
		event.InstallerFunc = nativeInstaller(setupOpenMode)
		noteServerBuildStart(event.App)
		letGoOfPassedMigrationRecoveryPoints(event.App)
		warnUnenforcedOrganizationPolicies(event.App)
		warnBackupsOff(event.App)
		settleEarlierOwnerElsewhere(event.App)
		if err := registerMemoryPressure(event); err != nil {
			return err
		}
		// JSON answers say they are UTF-8 (response_json_charset.go).
		registerResponseJSONCharset(event)
		registerAPIErrorForm(event)
		// Once the server is stopping, requests are told so (server_stopping.go),
		// and the writes of those under way stop with it (request_write_stop.go).
		registerServerStoppingAnswer(event)
		registerRequestWriteStop(event)
		registerRequestLimits(event)
		registerRequestAdmission(event)
		registerResponseCompression(event)
		registerWriteLane(event)
		if err := registerDataEpochGuard(event); err != nil {
			return err
		}
		registerClientHeaderExposure(event)
		registerBootstrapVisibilityProjectionWake(event, bootstrapVisibilityProjection)
		registerPublicShareGraphUpkeepWake(event, publicShareGraphUpkeep)
		event.Router.BindFunc(hidePocketBaseProductSurfaces)
		event.Router.BindFunc(redactRequestLogQuery)
		registerRequestLogPolicy(event)
		registerNetworkProxy(event)
		registerNativeIDAliases(event)
		registerUserKeyAuthentication(event)
		registerPersonRequestLanguage(event)
		registerNativeOperationDispatcher(event)
		registerReadBatchRoute(event)
		registerBrowserSessionAuthority(event)
		registerOrganizationUserKeyPolicyRoutes(event)
		registerEventWebhookRoutes(event)
		registerAppRegistrationRoutes(event)
		registerWorkspaceAppRoutes(event)
		registerAppPlatformRoutes(event)
		registerMCPServiceRoutes(event)
		appPlatformShutdown = startAppPlatformUpkeep(event.App)
		registerViewLinkRoutes(event)
		registerAccessRequestRoutes(event)
		registerAuthRoutes(event)
		registerOrganizationAuditRoutes(event)
		registerAuthAuditRoutes(event)
		registerPageFileSaveRoutes(event)
		registerAdministratorRoutes(event, administratorMutations)
		registerAdministratorAccountRoutes(event)
		registerAdministratorInstanceReportRoutes(event)
		registerAdministratorBackupRoutes(event, jobs)
		registerScheduledBackupRoutes(event)
		registerPersonStepRoutes(event)
		registerInstanceProductExportRoutes(event, jobs)
		registerDatabaseMutationDeletionRoutes(event)
		registerDatabasePropertyConversionRoutes(event, ygoPersistence)
		registerDatabaseDataSourceRoutes(event, ygoPersistence)
		registerDeletedDatabaseRoutes(event)
		registerDataSourceMentionPreviewRoute(event)
		registerWorkspacePermissionGroupRoutes(event)
		registerWorkspacePermissionGroupDetailRoutes(event)
		registerDatabaseStarterRoutes(event, ygoPersistence)
		registerDatabasePropertyTrashRoutes(event, ygoPersistence)
		registerProductRoutes(event, jobs, roomAuthorization, ygoPersistence)
		registerCollaborationStorageStatusRoute(event, ygoPersistence)
		registerCollaborationStateVectorRoute(event)
		registerInteractiveAutomationRoutes(event, ygoPersistence)
		registerMCPRoutes(event, mcpSSESessions)
		registerNotionAdminAPIRoutes(event, jobs)
		registerNotionCompatibilityRoutes(event, ygoPersistence)
		registerNotionAdminTokenRoutes(event)
		registerURLMetadataRoutes(event, urlMetadata)
		registerHealthRoutes(event)
		registerServerVersionRoute(event)
		event.Router.GET(apiRoot+"/collaboration/{room}", func(request *core.RequestEvent) error {
			roomAuthorization.ServeHTTP(request.Response, request.Request)
			return nil
		})
		// The same for every other method: an API path no route answers says
		// so in the API's own shape, not the framework's (2026-11-07: a POST
		// to one answered {"data":{},"message":…}).
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			event.Router.Route(method, apiRoot+nativeMissingRoutePath, func(request *core.RequestEvent) error {
				return apiError(request, http.StatusNotFound, errorCodeAPIRouteNotFound, "This server has no such API.")
			})
		}
		static := apis.Static(web.Dist(), true)
		buildFiles := apis.Static(web.Dist(), false)
		event.Router.GET(webAppPath, func(request *core.RequestEvent) error {
			// An API path no route answers is a missing API, not a page of the
			// app — as the organization module's paths are on the core alone.
			if strings.HasPrefix(request.Request.URL.Path, apiRoot+"/") {
				return apiError(request, http.StatusNotFound, errorCodeAPIRouteNotFound, "This server has no such API.")
			}
			setLeafenDocumentPolicy(request.Response.Header(), "")
			// A build file this build doesn't have — an older build's module a
			// tab still running it asks for — is missing, not a page of the
			// app, whose page in its place would leave the module unloaded.
			if webBuildFilePath(request.Request.URL.Path) {
				return serveWebBuildFile(request, buildFiles)
			}
			// The app's page names this build's modules: a browser asks again
			// before using a copy it kept, which an older build's would name
			// modules this server no longer has (2026-11-05: a tab Chrome put
			// away and opened after a new build stayed blank). Asked again, it
			// is sent whole, never "not modified" by its file's time: a server
			// that gives its page one (a development build reading the web
			// build from disk; an embedded build gives none) answered so a
			// browser holding a newer build's page once it went back to an
			// older build, and the newer page's modules were asked for in vain
			// (2026-11-18).
			request.Response.Header().Set("Cache-Control", "no-cache")
			request.Request.Header.Del("If-Modified-Since")
			// The app's own document is headed with its link preview (link_preview_page.go).
			if appDocumentPath(request.Request.URL.Path) {
				return serveAppDocument(request)
			}
			return static(request)
		})
		if err := event.Next(); err != nil {
			return err
		}
		// Every route is registered by now.
		if err := checkUnclassifiedKeyRoutes(event.App, event.Router); err != nil {
			return err
		}
		logReadKeyWritesHeld(event.App, event.Router)
		jobs.Start()
		notionRetention.Start()
		blockTrashRetention.Start()
		authSessionCleanup.Start()
		pageInteractionRetention.Start()
		pageAnalyticsRetention.Start()
		notificationMaterialization.Start()
		notificationEmails.Start()
		webPush.Start()
		slackNotifications.Start()
		eventWebhooks.Start()
		notificationDigests.Start()
		reminders.Start()
		peopleGuestRows.Start()
		databaseSprints.Start()
		pageDeletions.Start()
		searchIndexer.Start()
		// Graph reads' snapshots read from a pool of their own (read_snapshot.go).
		if pool, err := openReadSnapshotPool(pb); err != nil {
			pb.Logger().Warn("read snapshot pool unavailable; reads use transactions", "error", err)
		} else {
			pb.Store().Set(readSnapshotPoolStoreKey, pool)
		}
		// The data's read pools keep connections open (sqlite_read_connections_kept.go).
		readConnectionsKept.Start()
		bootstrapVisibilityProjection.Start()
		publicShareGraphUpkeep.Start()
		wikiVerification.Start()
		mcpCredentialRegistry.Start()
		fileMaintenance.Start()
		fileText.Start()
		notionFileUploadImport.Start()
		fileManualCleanup.Start()
		releaseManifest.Start()
		scheduledBackups.Start()
		ygoCompaction.Start()
		databaseMutationDeletion.Start()
		databasePropertyConversion.Start()
		databaseRelationPairs.Start()
		databaseUniqueIDs.Start()
		peopleValueInvitees.Start()
		roomAuthorization.StartAccessRecheck()
		// Sign-ins the command line ends reach open screens (sign_in_ends_follow.go).
		signInEnds.Start(event.App)
		startOrganizationComponents()
		// What requests left to do when the server last stopped goes on (product_afterwork_kept.go).
		resumeProductAfterwork(event.App)
		return nil
	})

	pb.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id:       "leafenProductShutdown",
		Priority: -10000,
		Func: func(event *core.TerminateEvent) error {
			// Requests that come from here on are told the server is busy,
			// and those under way that fail are the server's, not their
			// clients' (server_stopping.go).
			markServerStopping(event.App)
			// Nor does the server's clock begin a job (server_background.go).
			event.App.Cron().Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// When each kind of write is stopped, in order (shutdown.go).
			stop := serverStopOf(ctx)
			// The tunnel closes first, so no one outside reaches a closing server.
			serverTunnelOf(event.App).stopFollowing()
			serverTunnelOf(event.App).stop()
			// The collaboration's writes and the requests' under way get until
			// half the time to end; those still running are then stopped.
			requestWritesStopped := make(chan struct{})
			go func() {
				defer close(requestWritesStopped)
				stopRequestWrites(ctx, event.App, stop)
			}()
			ygoPersistence.beginShutdown(ctx, stop)
			<-requestWritesStopped
			// Waits for the write connection end once the writes the stop
			// ends were stopped, the background's among them (sqlite_write_gate.go).
			closeWriteConnectionWaits(event.App, stop.waits)
			collaborationShutdown := func(ctx context.Context) error {
				if err := collaboration.Shutdown(ctx); err != nil {
					return fmt.Errorf("%s: %w", errorCodeYgoShutdown, err)
				}
				return nil
			}
			// The organization module's runners stop before the core's.
			organizationErr := shutdownOrganizationComponents(ctx)
			shutdownErr := shutdownProductComponents(
				ctx,
				// Work requests left to do ends first, while the database is open.
				productAfterworkOf(event.App).Shutdown,
				// The background work no runner owns (server_background.go).
				serverBackgroundOf(event.App).Shutdown,
				administratorMutations.Shutdown,
				jobs.Shutdown,
				notionRetention.Shutdown,
				blockTrashRetention.Shutdown,
				fileText.Shutdown,
				authSessionCleanup.Shutdown,
				signInEnds.Shutdown,
				lastWriteMayBeLost(event.App, "login sessions' last uses", writeAuthSessionUses),
				pageInteractionRetention.Shutdown,
				pageAnalyticsRetention.Shutdown,
				notificationMaterialization.Shutdown,
				notificationEmails.Shutdown,
				webPush.Shutdown,
				slackNotifications.Shutdown,
				eventWebhooks.Shutdown,
				notificationDigests.Shutdown,
				reminders.Shutdown,
				peopleGuestRows.Shutdown,
				databaseSprints.Shutdown,
				pageDeletions.Shutdown,
				searchIndexer.Shutdown,
				readConnectionsKept.Shutdown,
				closeReadSnapshotPool(event.App),
				bootstrapVisibilityProjection.Shutdown,
				publicShareGraphUpkeep.Shutdown,
				wikiVerification.Shutdown,
				mcpSSESessions.Shutdown,
				mcpCredentialRegistry.Shutdown,
				urlMetadata.Shutdown,
				notionFileUploadImport.Shutdown,
				fileManualCleanup.Shutdown,
				fileMaintenance.Shutdown,
				releaseManifest.Shutdown,
				scheduledBackups.Shutdown,
				databaseMutationDeletion.Shutdown,
				databasePropertyConversion.Shutdown,
				databaseRelationPairs.Shutdown,
				databaseUniqueIDs.Shutdown,
				peopleValueInvitees.Shutdown,
				roomAuthorization.ShutdownAccessRecheck,
				func(ctx context.Context) error { return appPlatformShutdown(ctx) },
				ygoCompaction.Shutdown,
				collaborationShutdown,
				ygoPersistence.settlement.Shutdown,
			)
			// The requests' writes still running end, and their requests are
			// answered, before the listener closes (request_write_stop.go).
			endRequestWrites(ctx, event.App)
			// The read log writes what it gathered once the rest has stopped
			// reading, before the replica's last sending (read_log_buffer.go).
			readLogErr := closeReadLog(event.App)
			if readLogErr != nil {
				readLogErr = fmt.Errorf("leafen read log: the reads gathered were not written as the server stopped: %w", readLogErr)
			}
			// The replica stops last, once nothing else writes, so the last
			// changes go out before the database closes: the database's, then
			// the files' (replica_session_stop.go).
			replicationCtx, cancelReplication := context.WithTimeout(context.Background(), 5*time.Second)
			replicationErr := replication.Shutdown(replicationCtx)
			cancelReplication()
			nextErr := event.Next()
			return errors.Join(
				organizationErr,
				shutdownErr,
				readLogErr,
				replicationErr,
				nextErr,
			)
		},
	})

	registerCommands(pb)
	attachOrganizationModules(pb)
	return pb
}

func bearerToken(request *http.Request) string {
	value := request.Header.Get("Authorization")
	prefix, token, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// webBuildFilePath says whether a path names one of the web build's files
// rather than a page of the app: anything in its folders of files, or a
// file at its root (a name with an extension, as favicon.ico or sw.js).
func webBuildFilePath(path string) bool {
	name := strings.TrimPrefix(path, "/")
	for _, folder := range []string{"assets/", "fonts/", "covers/"} {
		if strings.HasPrefix(name, folder) {
			return true
		}
	}
	return name != "" && !strings.Contains(name, "/") && strings.Contains(strings.TrimPrefix(name, "."), ".")
}
