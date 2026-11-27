package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// An organization's sign-in rules come before device registration (the
// multi-server decision of 2026-10-20): one that signs its people in through
// its SAML provider refuses a registered device's sign-in as any other
// (sso_sign_in_policy.go), and one that asks for a password and 2-step
// verification refuses a device that joined by a code. The organization
// module decides the second (DeviceSignIn); without it no device is refused.
// A code is refused when it is made, too, so no one hands over one that
// can't be used.

var errDeviceSignInRestricted = errors.New(errorCodeDeviceSignInRestricted)

// authorizeDeviceSignIn fails with errSAMLRequired or errDeviceSignInRestricted
// when the person's organization keeps them from signing in by a registered
// device.
func authorizeDeviceSignIn(app core.App, userID string) error {
	if err := authorizeEnterpriseSSOSignIn(app, userID, authSessionMethodDevice, ""); err != nil {
		return err
	}
	if check := organizationPolicy.DeviceSignIn; check != nil {
		return check(app, userID)
	}
	return nil
}
