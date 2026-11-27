package main

import (
	"errors"
	"sync"

	"leafen.org/leafen/internal/managedsettings"
)

// managedSettings are what the computer's organization set through device
// management (internal/managedsettings) — the server the app opens — read
// once as the app starts.
var managedSettings = sync.OnceValue(managedsettings.Read)

var errManagedSetting = errors.New("this computer's organization set that")
