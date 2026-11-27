package app

import (
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The trusted sponsor list (or the one bundled with the release), each
// sponsor by name, logo and one-line slogan, with its share and Leafen's own
// slot among them, handed on in the runtime settings for the home's sponsors
// row to come after release; no screen shows it now (user decision
// 2026-11-25: the in-app credit — the loading screen's line, the workspace
// menu's row, the sign-in draw and the administrator switch — was taken
// out). A list's sponsors stand for 45 days from its issue: after that only
// Leafen's slot stays, so a company that stopped sponsoring doesn't stay on
// a server that no longer updates. A sponsor's link goes through Leafen's
// site, which sends it on to the sponsor, so a sponsor's site that turns bad
// is switched off at once on every server. LEAFEN_SPONSOR_CREDIT=off, the one
// switch, empties the list, and with it takes away the workspace menu's
// GitHub line (web/src/lib/sponsorCredit.ts) — for a partner whose company
// workspace shows no one else's name.

const (
	sponsorCreditEnv = "LEAFEN_SPONSOR_CREDIT"
	// sponsorCreditGoAddress is where a sponsor's credit links, the
	// sponsor's id after it.
	sponsorCreditGoAddress = "https://leafen.org/go/"
	sponsorListStands      = 45 * 24 * time.Hour
)

func sponsorCreditDisabledByEnvironment() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(sponsorCreditEnv)), "off")
}

// publicRuntimeSponsorCredits lists the sponsors with each one's share; the
// shares add up to one. Empty only with LEAFEN_SPONSOR_CREDIT=off.
func publicRuntimeSponsorCredits(app core.App) []publicRuntimeSponsorCredit {
	if sponsorCreditDisabledByEnvironment() {
		return []publicRuntimeSponsorCredit{}
	}
	return sponsorCreditsOf(currentSponsorList(app), time.Now())
}

func sponsorCreditsOf(list sponsorList, now time.Time) []publicRuntimeSponsorCredit {
	issued, err := time.Parse(time.RFC3339, list.IssuedAt)
	if err != nil || now.Sub(issued) > sponsorListStands {
		return []publicRuntimeSponsorCredit{{Kind: "house", URL: list.House.URL, Weight: 1}}
	}
	credits := []publicRuntimeSponsorCredit{{
		Kind: "house", URL: list.House.URL, Weight: float64(list.House.Share) / releaseShareWhole,
	}}
	for _, sponsor := range list.Sponsors {
		credit := publicRuntimeSponsorCredit{
			Kind: "sponsor", Name: sponsor.Name, URL: sponsorCreditGoAddress + sponsor.ID, Logo: sponsor.Logo,
			Weight: float64(sponsor.Share) / releaseShareWhole,
		}
		if sponsor.Slogan != nil {
			credit.Slogan = sponsor.Slogan.Text
		}
		credits = append(credits, credit)
	}
	return credits
}
