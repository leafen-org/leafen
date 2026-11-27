package main

import (
	"errors"

	"github.com/pocketbase/pocketbase"

	"leafen.org/leafen/internal/app"
)

// DeviceOwner lets the person who owns this device back in to the app's own
// server when the window lost its sign-in (the multi-server decision of
// 2026-10-20): the system checks the owner — Touch ID, Face ID or the
// device's password, Windows Hello or the Windows account's password — and
// the shell then makes a one-time device code inside the server, which the
// window joins with. Where the system has no such check (Android), the
// window says where else a code comes from. A sign-in the window lost
// without its person signing out needs no check: the window signs the person
// who set the server up back in by itself (SignInBack).
type DeviceOwner struct {
	local *localServer
}

// DeviceOwnerSignIn is what came of a check: "code" with the code to join
// with, "cancelled", "unavailable", or "refused" with the error code the
// account's organization answered.
type DeviceOwnerSignIn struct {
	Outcome string `json:"outcome"`
	Code    string `json:"code,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// deviceOwnerCheck is what the system answered.
type deviceOwnerCheck int

const (
	deviceOwnerChecked deviceOwnerCheck = iota
	deviceOwnerCancelled
	deviceOwnerUnavailable
)

func newDeviceOwner(local *localServer) *DeviceOwner { return &DeviceOwner{local: local} }

// Available says whether this device can check its owner.
func (*DeviceOwner) Available() bool { return deviceOwnerCheckAvailable() }

// Accounts lists the accounts on the app's own server to sign back in to.
func (owner *DeviceOwner) Accounts() ([]app.DeviceOwnerAccount, error) {
	product, err := owner.product()
	if err != nil {
		return nil, err
	}
	return app.DeviceOwnerAccounts(product)
}

// SignIn asks the system to check the device's owner, with reason shown in
// its prompt, and then makes a code for the account.
func (owner *DeviceOwner) SignIn(accountID, reason string) DeviceOwnerSignIn {
	product, err := owner.product()
	if err != nil {
		return DeviceOwnerSignIn{Outcome: "unavailable"}
	}
	switch checkDeviceOwner(reason) {
	case deviceOwnerUnavailable:
		return DeviceOwnerSignIn{Outcome: "unavailable"}
	case deviceOwnerCancelled:
		return DeviceOwnerSignIn{Outcome: "cancelled"}
	}
	code, err := app.IssueDeviceOwnerCode(product, accountID)
	switch {
	case errors.Is(err, app.ErrSAMLRequired):
		return DeviceOwnerSignIn{Outcome: "refused", Reason: "LEAFEN_SAML_REQUIRED"}
	case errors.Is(err, app.ErrDeviceSignInRestricted):
		return DeviceOwnerSignIn{Outcome: "refused", Reason: "LEAFEN_DEVICE_SIGN_IN_RESTRICTED"}
	case err != nil:
		return DeviceOwnerSignIn{Outcome: "refused"}
	}
	return DeviceOwnerSignIn{Outcome: "code", Code: code}
}

// SignInBack makes a code for the person who set the app's own server up,
// with no system check: the window asks it as it lands on that server's
// sign-in page without the person having signed out there — a sign-in it
// lost, whatever the cause, never keeps them out (user decision 2026-11-19
// "넣어"). After a sign-out the person asked for, the window doesn't ask, and
// the owner check (SignIn) is the way back. "unavailable" where the server
// isn't set up or answers no one to sign in.
func (owner *DeviceOwner) SignInBack() DeviceOwnerSignIn {
	product, err := owner.product()
	if err != nil {
		return DeviceOwnerSignIn{Outcome: "unavailable"}
	}
	code, err := app.SetupOwnerSignInBackCode(product)
	switch {
	case errors.Is(err, app.ErrSAMLRequired):
		return DeviceOwnerSignIn{Outcome: "refused", Reason: "LEAFEN_SAML_REQUIRED"}
	case errors.Is(err, app.ErrDeviceSignInRestricted):
		return DeviceOwnerSignIn{Outcome: "refused", Reason: "LEAFEN_DEVICE_SIGN_IN_RESTRICTED"}
	case err != nil:
		return DeviceOwnerSignIn{Outcome: "refused"}
	case code == "":
		return DeviceOwnerSignIn{Outcome: "unavailable"}
	}
	return DeviceOwnerSignIn{Outcome: "code", Code: code}
}

var errDeviceOwnerServerOff = errors.New("this device serves no workspace of its own now")

// product is the app's own server once it answers.
func (owner *DeviceOwner) product() (*pocketbase.PocketBase, error) {
	ready, err, _, _ := owner.local.current()
	select {
	case <-ready:
	default:
		return nil, errDeviceOwnerServerOff
	}
	if err != nil {
		return nil, err
	}
	owner.local.mu.RLock()
	product := owner.local.product
	owner.local.mu.RUnlock()
	if product == nil {
		return nil, errDeviceOwnerServerOff
	}
	return product, nil
}
