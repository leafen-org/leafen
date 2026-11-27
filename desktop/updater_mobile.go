//go:build ios || android

package main

import "context"

// Updater does nothing on a phone: its store updates the app.
type Updater struct{}

func newUpdater() *Updater                { return &Updater{} }
func (*Updater) ReadyAtStart() bool       { return false }
func (*Updater) HandOffAtStart(bool) bool { return false }
func (*Updater) Run(context.Context)      {}
func (*Updater) MarkHealthy()             {}
func (*Updater) MarkUpdating()            {}
func runUpdateHelper([]string) int        { return 2 }
