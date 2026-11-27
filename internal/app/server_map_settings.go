package app

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"unicode"
)

// Map views draw street-level tiles that each browser fetches straight from
// the tile server, OpenStreetMap's public tiles unless the administrator
// names another server. The administrator, or LEAFEN_MAP_TILES_DISABLED, can
// turn external tiles off; maps then draw only the outline bundled with the
// product. Tiles always show their attribution.

const (
	defaultMapTileURL        = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	defaultMapAttribution    = "© OpenStreetMap contributors"
	defaultMapAttributionURL = "https://www.openstreetmap.org/copyright"
	mapTilesDisabledEnv      = "LEAFEN_MAP_TILES_DISABLED"
)

func mapTilesDisabledByEnvironment() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(mapTilesDisabledEnv))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func projectAdministratorMapSettings(settings storedAdministratorSettings) administratorMapSettings {
	return administratorMapSettings{
		Enabled: settings.MapTilesEnabled, TileURL: settings.MapTileURL, Attribution: settings.MapAttribution,
		AttributionURL: settings.MapAttributionURL, EnvironmentDisabled: mapTilesDisabledByEnvironment(),
	}
}

// publicRuntimeMap is what every browser needs to draw map tiles: an empty
// tile address when external tiles are off.
func publicRuntimeMap(settings storedAdministratorSettings, loaded bool) publicRuntimeMapConfig {
	if !loaded || !settings.MapTilesEnabled || mapTilesDisabledByEnvironment() {
		return publicRuntimeMapConfig{}
	}
	if settings.MapTileURL == "" {
		return publicRuntimeMapConfig{
			TilesEnabled: true, TileURL: defaultMapTileURL,
			Attribution: defaultMapAttribution, AttributionURL: defaultMapAttributionURL,
		}
	}
	return publicRuntimeMapConfig{
		TilesEnabled: true, TileURL: settings.MapTileURL,
		Attribution: settings.MapAttribution, AttributionURL: settings.MapAttributionURL,
	}
}

// normalizeAdministratorMapInput checks a map settings change. A custom tile
// address needs the {z}, {x} and {y} placeholders and an attribution; an
// empty address means OpenStreetMap's public tiles.
func normalizeAdministratorMapInput(input *administratorMapSettingsInput) error {
	if input == nil {
		return nil
	}
	input.TileURL = strings.TrimSpace(input.TileURL)
	input.Attribution = strings.TrimSpace(input.Attribution)
	input.AttributionURL = strings.TrimSpace(input.AttributionURL)
	invalid := errors.New(errorCodeAdministratorSettingsInvalid)
	if len(input.TileURL) > maxAdministratorMapURLLength || len(input.AttributionURL) > maxAdministratorMapURLLength ||
		len([]rune(input.Attribution)) > maxAdministratorMapAttributionLength ||
		strings.IndexFunc(input.Attribution, unicode.IsControl) >= 0 {
		return invalid
	}
	if input.TileURL == "" {
		input.Attribution, input.AttributionURL = "", ""
		return nil
	}
	if !validAdministratorMapURL(input.TileURL) || input.Attribution == "" {
		return invalid
	}
	for _, placeholder := range []string{"{z}", "{x}", "{y}"} {
		if !strings.Contains(input.TileURL, placeholder) {
			return invalid
		}
	}
	if input.AttributionURL != "" && !validAdministratorMapURL(input.AttributionURL) {
		return invalid
	}
	return nil
}

func validAdministratorMapURL(raw string) bool {
	parsed, err := url.Parse(strings.NewReplacer("{z}", "0", "{x}", "0", "{y}", "0").Replace(raw))
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" &&
		parsed.User == nil && parsed.Fragment == "" && strings.IndexFunc(raw, unicode.IsSpace) < 0
}

// applyAdministratorMapInput carries the stored map settings over when a
// settings change leaves the map section out.
func applyAdministratorMapInput(next *storedAdministratorSettings, input *administratorMapSettingsInput, current storedAdministratorSettings) {
	if input == nil {
		next.MapTilesEnabled, next.MapTileURL = current.MapTilesEnabled, current.MapTileURL
		next.MapAttribution, next.MapAttributionURL = current.MapAttribution, current.MapAttributionURL
		return
	}
	next.MapTilesEnabled, next.MapTileURL = input.Enabled, input.TileURL
	next.MapAttribution, next.MapAttributionURL = input.Attribution, input.AttributionURL
}
