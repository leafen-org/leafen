//go:build !darwin && !windows

package main

// Android's window keeps its sign-ins with the app's own Keystore store
// (LeafenCredentials.java); other systems keep none.
func platformSecretStore() secretStore { return nil }
