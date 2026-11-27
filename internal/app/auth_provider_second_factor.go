package app

// A sign-in provider's own 2-step assurance (user decision 2026-10-10: a
// session that went through a second factor counts, including one its sign-in
// provider vouches for). Taken conservatively, only from what Leafen verified
// itself: an OIDC ID token checked against the provider's keys, issuer and
// nonce whose amr (RFC 8176) says "mfa"; a signed SAML assertion whose
// authentication context, or Microsoft's authentication methods attribute,
// names multi-factor authentication. Factor lists that may be one factor
// ("otp", "hwk", a smartcard class) don't count. A session keeps it as
// "provider" beside the 2-step code and passkey.

const authSessionProviderSecondFactor = "provider"

// authSessionSecondFactorColumns stores a session's second factor: a code or
// passkey in its own column, a provider's assurance as its flag.
func authSessionSecondFactorColumns(factor string) (string, int) {
	if factor == authSessionProviderSecondFactor {
		return "", 1
	}
	return factor, 0
}

// authSessionSecondFactorSQL reads a session's second factor back, "" for none.
const authSessionSecondFactorSQL = `CASE WHEN [[second_factor]] = '' AND [[provider_second_factor]] = 1
	THEN 'provider' ELSE [[second_factor]] END`

// authSessionWithoutSecondFactorSQL is true of a session that went through none.
const authSessionWithoutSecondFactorSQL = `([[second_factor]] = '' AND [[provider_second_factor]] = 0)`

func authProviderSecondFactorFlag(assured bool) int {
	if assured {
		return 1
	}
	return 0
}

func authProviderSecondFactor(flag int) string {
	if flag == 1 {
		return authSessionProviderSecondFactor
	}
	return ""
}

// oidcAMRAssertsMFA is true of an ID token's amr that names "mfa".
func oidcAMRAssertsMFA(value any) bool {
	methods, ok := value.([]any)
	if !ok {
		return false
	}
	for _, method := range methods {
		if text, _ := method.(string); text == "mfa" {
			return true
		}
	}
	return false
}
