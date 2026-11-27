package app

// What an app's workers ran (docs/app-platform.md 10·11): each run with its
// trigger, its subject, its outcome, how long it took and what it logged,
// and for each incoming hook call the attempts its worker made.

const appRunsTable = "leafen_app_runs"

const appApprovalsTable = "leafen_action_approvals"
