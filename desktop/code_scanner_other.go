//go:build !ios

package main

// Only the iOS app scans through the shell; Android's window has its own
// scanner and a computer none.
func scanCode() CodeScan { return CodeScan{Outcome: "unavailable"} }
