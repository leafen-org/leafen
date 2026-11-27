//go:build !darwin && !windows

package main

// Android has no owner check here: the window says where else a code comes
// from.

func deviceOwnerCheckAvailable() bool { return false }

func checkDeviceOwner(string) deviceOwnerCheck { return deviceOwnerUnavailable }
