//go:build android

package main

// On Android the page brings a sign-in back through the app's own bridge
// (LeafenSignIn.java): the system browser returns to the app's scheme, which
// opens the app.

func listenForSignIn(string, func(string)) (string, func(), error) {
	return "", nil, errSignInUnavailable
}

func openSignIn(string, string, func(string)) error {
	return errSignInUnavailable
}
