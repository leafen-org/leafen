package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// An organization may require its members to sign in through its SAML
// provider. The organization module decides that (single sign-on); without
// it no sign-in is refused for it. The sign-in paths say how the person signs
// in.

const (
	samlAuthenticationMethodPassword = "password"
	samlAuthenticationMethodOAuth    = "oauth"
	samlAuthenticationMethodSAML     = "saml"
)

var (
	errSAMLRequired             = errors.New(errorCodeSAMLRequired)
	errSAMLConfigurationInvalid = errors.New(errorCodeSAMLConfigurationInvalid)
)

// authorizeEnterpriseSSOSignIn fails with errSAMLRequired when an
// organization requires the person to sign in through its SAML provider and
// this sign-in is not that.
func authorizeEnterpriseSSOSignIn(app core.App, userID string, method string, providerID string) error {
	if userID == "" {
		return errSAMLRequired
	}
	if check := organizationPolicy.SSOSignIn; check != nil {
		return check(app, userID, method, providerID)
	}
	return nil
}
