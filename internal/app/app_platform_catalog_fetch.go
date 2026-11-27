//go:build !leafen_noapps

package app

import (
	"context"
	"net/url"
)

// fetchVerifiedAppCatalog reads an address's index and signature. A list key
// that can't be one is refused before anything is asked of the address, as
// an address that can't be is (openAppCatalogURL), saying why: no request
// leaves for a catalog that couldn't be trusted whatever it answered
// (2026-11-13: a key of the wrong form still sent a GET to the address first).
func fetchVerifiedAppCatalog(ctx context.Context, address, keyText string) ([]byte, appCatalogIndex, error) {
	// The address first, as the form asks it before the key.
	if parsed, err := url.Parse(address); err != nil || !appCatalogURLAllowed(parsed) {
		return nil, appCatalogIndex{}, errAppCatalogAddressRefused
	}
	if _, err := parseAppCatalogKey(keyText); err != nil {
		return nil, appCatalogIndex{}, err
	}
	index, err := fetchAppCatalogBytes(ctx, address, appCatalogIndexMaxBytes)
	if err != nil {
		return nil, appCatalogIndex{}, err
	}
	signature, err := fetchAppCatalogBytes(ctx, address+".sig", 4096)
	if err != nil {
		return nil, appCatalogIndex{}, err
	}
	parsed, err := verifyAppCatalog(keyText, index, signature)
	return index, parsed, err
}
