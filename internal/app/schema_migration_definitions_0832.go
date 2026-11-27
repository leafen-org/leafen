package app

// The product migrations after the schema baseline, from version 832 on, in
// version order (schema_migration_definitions.go says what a migration is).
// A new migration goes at the end, with the next version, a name starting
// native_ and its checksum beside it. The baseline stays as it is and a new database runs these after
// it one by one; only the export policy's version follows the last one
// (node scripts/generate-schema-migration-impact.mjs --write, which the web
// build checks). One that adds a table the export policy carries puts the
// export's tracking triggers on it (trackInstanceProductExportSourceRevisions);
// one that takes away or reshapes what the baseline made lists it in
// schemaBaselineRetired.
var productMigrationsFrom0832 = []productMigration{
	{version: 832, name: "native_retire_earlier_wiki_listing", checksum: nativeRetireEarlierWikiListingChecksum, impact: schemaMigrationImpact("BPR-026", "instance-product-export"), up: dropEarlierWikiListing, validate: validateEarlierWikiListingDropped},
	{version: 833, name: "native_retire_workspace_activity_backfill", checksum: nativeRetireWorkspaceActivityBackfillChecksum, impact: schemaMigrationImpact("BPR-010", "instance-product-export"), up: dropWorkspaceActivityBackfill, validate: validateWorkspaceActivityBackfillDropped},
	{version: 834, name: "native_mcp_notion_tools", checksum: nativeMCPNotionToolsChecksum, impact: schemaMigrationImpact("BPR-020", "instance-product-export"), up: ensureNativeMCPNotionToolsSchema, validate: validateNativeMCPNotionToolsSchema},
	{version: 835, name: "native_retire_areas", checksum: nativeRetireAreasChecksum, impact: schemaMigrationImpact("BPR-022,BPR-027", "instance-product-export"), up: retireAreas, validate: validateAreasRetired},
	{version: 836, name: "native_request_controls", checksum: nativeRequestControlsChecksum, impact: schemaMigrationImpact("BPR-009,BPR-021,BPR-027", "instance-product-export"), up: ensureNativeRequestControlsSchema, validate: validateNativeRequestControlsSchema},
	{version: 837, name: "native_page_unlink_keeps_connections", checksum: nativePageUnlinkKeepsConnectionsChecksum, impact: schemaMigrationImpact("BPR-021,BPR-036", "instance-product-export"), content: copyConnectionsOntoUnlinkedPages, validate: validateConnectionsOntoUnlinkedPages},
	{version: 838, name: "native_internal_connection_token_grace", checksum: nativeInternalConnectionTokenGraceChecksum, impact: schemaMigrationImpact("BPR-021", "instance-product-export"), up: ensureNativeInternalConnectionTokenGraceSchema, validate: validateNativeInternalConnectionTokenGraceSchema},
	{version: 839, name: "native_retire_app_agent_form_answers", checksum: nativeRetireAppAgentFormAnswersChecksum, impact: schemaMigrationImpact("BPR-020,BPR-043", "instance-product-export"), up: dropAppAgentFormAnswers, validate: validateAppAgentFormAnswersDropped},
	{version: 840, name: "native_comment_tombstones", checksum: nativeCommentTombstonesChecksum, impact: schemaMigrationImpact("BPR-010,BPR-021", "instance-product-export"), up: ensureNativeCommentTombstonesSchema, validate: validateNativeCommentTombstonesSchema},
	{version: 841, name: "native_synced_copy_leftover_blocks", checksum: nativeSyncedCopyLeftoverBlocksChecksum, impact: schemaMigrationImpact("BPR-008,BPR-021", "instance-product-export"), content: moveSyncedCopyLeftoverBlocks, validate: validateSyncedCopyLeftoverBlocks},
	{version: 842, name: "native_notion_import_kept_mappings", checksum: nativeNotionImportKeptMappingsChecksum, impact: schemaMigrationImpact("BPR-018,BPR-021", "instance-product-export"), up: ensureNativeNotionImportKeptMappingsSchema, validate: validateNativeNotionImportKeptMappingsSchema},
	{version: 843, name: "native_moved_linked_views_unnamed", checksum: nativeMovedLinkedViewsUnnamedChecksum, impact: schemaMigrationImpact("BPR-018,BPR-021", "instance-product-export"), content: clearMovedUntitledLinkedViewNames, validate: validateMovedUntitledLinkedViewNames},
	{version: 844, name: "native_moved_databases_unnamed", checksum: nativeMovedDatabasesUnnamedChecksum, impact: schemaMigrationImpact("BPR-018,BPR-021", "instance-product-export"), content: clearMovedDefaultSourceNames, validate: validateMovedDefaultSourceNames},
	{version: 845, name: "native_search_date_mention_words", checksum: nativeSearchDateMentionWordsChecksum, impact: schemaMigrationImpact("BPR-022", "instance-product-export"), content: requeueSearchDateMentionWords, validate: validateSearchDateMentionWordsRequeued},
	{version: 846, name: "native_comment_connections", checksum: nativeCommentConnectionsChecksum, impact: schemaMigrationImpact("BPR-010,BPR-021", "instance-product-export"), up: ensureNativeCommentConnectionsSchema, validate: validateNativeCommentConnectionsSchema},
	{version: 847, name: "native_search_row_child_lines", checksum: nativeSearchRowChildLinesChecksum, impact: schemaMigrationImpact("BPR-022", "instance-product-export"), content: requeueSearchRowChildLines, validate: validateSearchRowChildLinesRequeued},
	{version: 848, name: "native_person_steps", checksum: nativePersonStepsChecksum, impact: schemaMigrationImpact("BPR-020,BPR-021", "instance-product-export"), up: ensureNativePersonStepsSchema, validate: validateNativePersonStepsSchema},
	{version: 849, name: "native_scheduled_backup_last_run", checksum: nativeScheduledBackupLastRunChecksum, impact: schemaMigrationImpact("BPR-037", "instance-product-export"), up: ensureNativeScheduledBackupLastRunSchema, validate: validateNativeScheduledBackupLastRunSchema},
	{version: 850, name: "native_connection_reach", checksum: nativeConnectionReachChecksum, impact: schemaMigrationImpact("BPR-021", "instance-product-export"), up: ensureNativeConnectionReachSchema, validate: validateNativeConnectionReachSchema},
	{version: 851, name: "native_moved_template_names_unnamed", checksum: nativeMovedTemplateNamesUnnamedChecksum, impact: schemaMigrationImpact("BPR-018,BPR-021", "instance-product-export"), content: clearMovedTemplateNames, validate: validateMovedTemplateNames},
	{version: 852, name: "native_notion_migration_format_orders", checksum: nativeNotionMigrationFormatOrdersChecksum, impact: schemaMigrationImpact("BPR-018,BPR-021", "instance-product-export"), up: ensureNativeNotionMigrationFormatOrdersSchema, validate: validateNativeNotionMigrationFormatOrdersSchema},
	{version: 853, name: "native_page_guest_links", checksum: nativePageGuestLinksChecksum, impact: schemaMigrationImpact("BPR-004,BPR-036,BPR-038", "instance-product-export"), up: ensureNativePageGuestLinksSchema, validate: validateNativePageGuestLinksSchema},
	{version: 854, name: "native_agent_sign_in_request_origin", checksum: nativeAgentSignInRequestOriginChecksum, impact: schemaMigrationImpact("BPR-004,BPR-020", "instance-product-export"), up: ensureNativeAgentSignInRequestOriginSchema, validate: validateNativeAgentSignInRequestOriginSchema},
	{version: 855, name: "native_device_connection_widens", checksum: nativeDeviceConnectionWidensChecksum, impact: schemaMigrationImpact("BPR-020,BPR-021", "instance-product-export"), up: ensureNativeDeviceConnectionWidensSchema, validate: validateNativeDeviceConnectionWidensSchema},
	{version: 856, name: "native_workspace_carried_people", checksum: nativeWorkspaceCarriedPeopleChecksum, impact: schemaMigrationImpact("BPR-018,BPR-021", "instance-product-export"), up: ensureNativeWorkspaceCarriedPeopleSchema, validate: validateNativeWorkspaceCarriedPeopleSchema},
	{version: 857, name: "native_page_workspace_moves", checksum: nativePageWorkspaceMovesChecksum, impact: schemaMigrationImpact("BPR-008,BPR-021", "instance-product-export"), up: ensureNativePageWorkspaceMovesSchema, validate: validateNativePageWorkspaceMovesSchema},
	{version: 858, name: "native_connection_default_workspace", checksum: nativeConnectionDefaultWorkspaceChecksum, impact: schemaMigrationImpact("BPR-020,BPR-021", "instance-product-export"), up: ensureNativeConnectionDefaultWorkspaceSchema, validate: validateNativeConnectionDefaultWorkspaceSchema},
	{version: 859, name: "native_page_export_count_whole", checksum: nativePageExportCountWholeChecksum, impact: schemaMigrationImpact("BPR-008,BPR-021", "instance-product-export"), up: ensureNativePageExportCountWhole, validate: validateNativePageExportCountWhole},
	{version: 860, name: "native_people_guest_trash_checks", checksum: nativePeopleGuestTrashChecksChecksum, impact: schemaMigrationImpact("BPR-021", "instance-product-export"), up: ensureNativePeopleGuestTrashChecks, validate: validateNativePeopleGuestTrashChecks},
	{version: 861, name: "native_search_untitled_database_names", checksum: nativeSearchUntitledDatabaseNamesChecksum, impact: schemaMigrationImpact("BPR-022", "instance-product-export"), content: requeueSearchUntitledDatabaseNames, validate: validateSearchUntitledDatabaseNamesRequeued},
	{version: 862, name: "native_search_page_database_lines", checksum: nativeSearchPageDatabaseLinesChecksum, impact: schemaMigrationImpact("BPR-022", "instance-product-export"), content: requeueSearchPageDatabaseLines, validate: validateSearchPageDatabaseLinesRequeued},
	{version: 863, name: "native_comment_readers_present", checksum: nativeCommentReadersPresentChecksum, impact: schemaMigrationImpact("BPR-010,BPR-021", "instance-product-export"), up: ensureNativeCommentReadersPresentSchema, validate: validateNativeCommentReadersPresentSchema},
	{version: 864, name: "native_account_github_support_opened", checksum: nativeAccountGitHubSupportOpenedChecksum, impact: schemaMigrationImpact("BPR-038", "instance-product-export"), up: ensureNativeAccountGitHubSupportSchema, validate: validateNativeAccountGitHubSupportSchema},
	{version: 865, name: "native_notion_migration_second_copy", checksum: nativeNotionMigrationSecondCopyChecksum, impact: schemaMigrationImpact("BPR-018,BPR-021", "instance-product-export"), up: ensureNativeNotionMigrationSecondCopySchema, validate: validateNativeNotionMigrationSecondCopySchema},
	{version: 866, name: "native_person_connection_rights", checksum: nativePersonConnectionRightsChecksum, impact: schemaMigrationImpact("BPR-020,BPR-021", "instance-product-export"), up: ensureNativePersonConnectionKindsSchema, content: liftAgentConnections, validate: validateAgentConnectionsLifted},
	{version: 867, name: "native_person_screen_language", checksum: nativePersonScreenLanguageChecksum, impact: schemaMigrationImpact("BPR-038", "instance-product-export"), up: ensureNativePersonScreenLanguageSchema, validate: validateNativePersonScreenLanguageSchema},
}

const (
	nativeRetireEarlierWikiListingChecksum        = "f406c9d4968c85a5af637337699a072c832833ba16c8babdd2ac3485363aaac6"
	nativeRetireWorkspaceActivityBackfillChecksum = "af42faff52f4da56f721da685e50d31450859a54fb0a4d2e6c26a5d129c7c266"
	nativeMCPNotionToolsChecksum                  = "57e254b61f9fb166864e919923edf833f1b47e56401c5dd899bfe955787c8262"
	nativeRetireAreasChecksum                     = "8124c0265da3e2f5efcf06d45abd77176da99bb55ad1f1649d0b317492e25155"
	nativeRequestControlsChecksum                 = "40072246db747bd5b4cc9b16a1fd29c12a4410c11412f465945afe82c476a158"
	nativePageUnlinkKeepsConnectionsChecksum      = "7249aa4bcaefb70ad212112636afae47dfb769cb6272fc3937e80f891f449554"
	nativeInternalConnectionTokenGraceChecksum    = "9eadf2cb119abf36a58fa43e3cea88d80faf7f34735e24a26e388dc88feb0a96"
	nativeRetireAppAgentFormAnswersChecksum       = "dde79b626f8c93ef0c82a38dae0a1902faf68295deb034b5c282b1cf474fae8d"
	nativeCommentTombstonesChecksum               = "629861a4cb274214315585e1183d1a3d37209bc76acd4d00d555be66ac054b6b"
	nativeSyncedCopyLeftoverBlocksChecksum        = "bf60a5b9a895b04603a53de8f587501e489fb0c92edb662e25e8506b002b8134"
	nativeNotionImportKeptMappingsChecksum        = "0c7ff4ad3c652c638f6c8790a7e14db31e324e2565e73a06507fa4f974851792"
	nativeMovedLinkedViewsUnnamedChecksum         = "aea0ec8b745fb6f7febd802d4625b02ffe95ceae6fc3bfeb999714aa73e749fc"
	nativeMovedDatabasesUnnamedChecksum           = "06b9e2e8666d63bc6cd84bc49b3e559e2cd566566ffe7ae199da324ec04367a3"
	nativeSearchDateMentionWordsChecksum          = "a6c11726bd7a3257cbce81af6ec4b8090ccd379196cda3844b40ef68bb95b281"
	nativeCommentConnectionsChecksum              = "9f96fb3afb25d2e01491dfb35cdba2ed660f986fb88a096e09d51b72b06e20c9"
	nativeSearchRowChildLinesChecksum             = "49fae29339dbc697931cd7fc1cfc164ace505015aba66e8f94504d8a29d6def0"
	nativePersonStepsChecksum                     = "d21c8a1406ca0e3c43fc0d2be7a7750fd98973aedd3536e3578d0d833a82c37e"
	nativeScheduledBackupLastRunChecksum          = "09a49b6fcd975f770c299a053b0c28df4f7598362bbac028d61de4f7628e9979"
	nativeConnectionReachChecksum                 = "e3b9ddbcf2af4b57a6ff3c673221a45ef961e5dcd22e1510d7a46fb33c6e7afb"
	nativeMovedTemplateNamesUnnamedChecksum       = "fa02c8f0eccefbd7a9d2e006fc74b270245b196b35258dca232557aec6cf4ad7"
	nativeAgentSignInRequestOriginChecksum        = "e2125da2ff35dd78e0fe3c8aada92652fcea365d5f120821d1e5e99406e8e49e"
	nativeDeviceConnectionWidensChecksum          = "0e139fe65f69c4df0a0936127352cb200e8d94f2648cc12072ee061a045939c7"
	nativeWorkspaceCarriedPeopleChecksum          = "7a5bb995a61e02b1f20e4bb111a02c3aaf3e579c64645a3847ca3981d2e159d6"
	nativePageWorkspaceMovesChecksum              = "1f7abc7c10471d9fb26b6a5c88eeeca08e6d877ad992c0e93b38a67aa7cdadce"
	nativeConnectionDefaultWorkspaceChecksum      = "fa12f960cf4aaee43882e145491bd3fb8cccdbcd6377653288033bd9673c4d31"
	nativePageExportCountWholeChecksum            = "eec3880699b5de49dba1e776938b84b59030d33d76a3c0a1100c98a33df65d88"
	nativePeopleGuestTrashChecksChecksum          = "6cf5e467228c568b7daa9e094a9ec5fbd1387ec50ddf141786a5badfad5ee4bb"
	nativeSearchUntitledDatabaseNamesChecksum     = "1c8ba6557744f9169225d5bd81dac0af8a42087a803ab9a6e5c214c262947dac"
	nativeSearchPageDatabaseLinesChecksum         = "b6850ad67f54f47fa775686b50cb0721d142dc27b304a64f1fecaae1d0c3a44f"
	nativeCommentReadersPresentChecksum           = "4b59b1ab7ccd7fab4479c2d564d9a17a8a17168c575b5a52373caa8bf45b385c"
	nativeAccountGitHubSupportOpenedChecksum      = "3fdc229b674d9f3c33353dfcd6822c685b57a18c1d21a180cb15f0d1ed4ee289"
	nativeNotionMigrationSecondCopyChecksum       = "5482d90c808791e66518aa3131f2ced5c99fd4c48b67d489923538bf1d5195f2"
	nativePersonConnectionRightsChecksum          = "fff3dedb0755e75a2d2793d611600ec02e8b4766f4c76a11c264d7056441bc22"
	nativePersonScreenLanguageChecksum            = "765f1c1ae7ff84d02a113db9dd4e16da41b043cb794905484385d8af75376d7f"
)
