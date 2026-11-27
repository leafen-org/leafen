//go:build leafen_noapps

package app

import (
	"fmt"
	"io"
)

// RunAppWorker has no apps to run in a build without the app platform.
func RunAppWorker(_ io.Reader, stdout io.Writer) int {
	_, _ = fmt.Fprintln(stdout, "this build has no app platform")
	return 1
}

// appWorkerMode says this build runs no app workers.
const appWorkerMode = "none"
