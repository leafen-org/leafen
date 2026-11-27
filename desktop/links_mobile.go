//go:build ios || android

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// A phone runs the app once; the system hands it links itself.
func singleInstance() *application.SingleInstanceOptions {
	return nil
}
