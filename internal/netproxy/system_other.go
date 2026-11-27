//go:build !(darwin && !ios) && !windows

package netproxy

// platformSettings: elsewhere the environment is the system's proxy
// setting (HTTPS_PROXY and the rest).
func platformSettings() systemSettings { return systemSettings{} }
