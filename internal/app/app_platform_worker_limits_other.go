//go:build !leafen_noapps && !unix && !windows

package app

// limitAppWorkerProcess has nothing to narrow where the system has no
// per-process limits the worker can set on itself.
func limitAppWorkerProcess() {}
