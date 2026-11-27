package app

type authProviderClaims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	// SecondFactor: the provider vouched this sign-in used more than one factor.
	SecondFactor bool
}
