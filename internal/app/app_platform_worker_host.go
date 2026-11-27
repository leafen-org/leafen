//go:build !leafen_noapps

package app

// On a server or a desktop each app worker is a process of its own: the
// executable runs `leafen app-worker` (the desktop app too) before anything
// else, narrowed and with no data, settings or environment.

const appWorkerMode = "process"

var appWorkerLimits = appWorkerLimitSet{perApp: 2, atOnce: 4, memoryPages: 1024, maxMemoryPages: 4096, longRuns: 16}

func defaultAppWorkerStart() (*appWorkerConn, error) { return startAppWorkerProcess() }
