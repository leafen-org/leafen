package app

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

var errInvalidPlaceSearchSettings = errors.New(errorCodeAdministratorSettingsInvalid)

// Endpoint and credentials share the existing administrator secret key and
// are write-only in settings. A search accepts text, never an upstream URL.
type placeSearchConfig struct {
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"apiKey"`
	APIKeyID string `json:"apiKeyId"`
}

func placeSearchEnvironmentDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LEAFEN_PLACE_SEARCH_DISABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
func storedPlaceSearchConfig(app core.App, settings storedAdministratorSettings) (placeSearchConfig, error) {
	var config placeSearchConfig
	plain, err := decryptAdministratorSetting(app, settings.PlaceSearchConfigCipher)
	if err != nil {
		return config, err
	}
	if plain != "" {
		err = json.Unmarshal([]byte(plain), &config)
	}
	return config, err
}
func effectivePlaceSearchConfig(app core.App, settings storedAdministratorSettings) (placeSearchConfig, error) {
	config := placeSearchConfig{}
	if placeSearchEnvironmentDisabled() {
		return config, errPlaceSearchUnavailable
	}
	if provider := strings.TrimSpace(os.Getenv("LEAFEN_PLACE_SEARCH_PROVIDER")); provider != "" {
		config = placeSearchConfig{Provider: provider, Endpoint: strings.TrimSpace(os.Getenv("LEAFEN_PLACE_SEARCH_URL")), APIKey: strings.TrimSpace(os.Getenv("LEAFEN_PLACE_SEARCH_API_KEY")), APIKeyID: strings.TrimSpace(os.Getenv("LEAFEN_PLACE_SEARCH_API_KEY_ID"))}
	} else {
		if !settings.PlaceSearchEnabled {
			return config, errPlaceSearchUnavailable
		}
		var err error
		config, err = storedPlaceSearchConfig(app, settings)
		if err != nil {
			return config, err
		}
	}
	if err := normalizePlaceSearchConfig(&config); err != nil {
		return config, err
	}
	return config, nil
}
func projectAdministratorPlaceSearch(app core.App, settings storedAdministratorSettings) administratorPlaceSearchSettings {
	stored, _ := storedPlaceSearchConfig(app, settings)
	_, effectiveErr := effectivePlaceSearchConfig(app, settings)
	return administratorPlaceSearchSettings{Enabled: settings.PlaceSearchEnabled, EffectiveEnabled: effectiveErr == nil, Provider: stored.Provider, Configured: settings.PlaceSearchConfigCipher != "", EnvironmentConfigured: strings.TrimSpace(os.Getenv("LEAFEN_PLACE_SEARCH_PROVIDER")) != "", EnvironmentDisabled: placeSearchEnvironmentDisabled()}
}
func placeSearchAvailable(app core.App, settings storedAdministratorSettings, loaded bool) bool {
	if !loaded {
		return false
	}
	_, err := effectivePlaceSearchConfig(app, settings)
	return err == nil
}
func normalizePlaceSearchConfig(config *placeSearchConfig) error {
	invalid := errInvalidPlaceSearchSettings
	for _, value := range []string{config.Endpoint, config.APIKey, config.APIKeyID} {
		if len(value) > 2048 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return invalid
		}
	}
	switch config.Provider {
	case "nominatim", "photon":
		if config.APIKey != "" || config.APIKeyID != "" {
			return invalid
		}
	case "kakao":
		if config.Endpoint == "" {
			config.Endpoint = "https://dapi.kakao.com/v2/local/search/keyword.json"
		}
		if config.APIKey == "" || config.APIKeyID != "" {
			return invalid
		}
	case "naver":
		if config.Endpoint == "" {
			config.Endpoint = "https://maps.apigw.ntruss.com/map-geocode/v2/geocode"
		}
		if config.APIKey == "" || config.APIKeyID == "" {
			return invalid
		}
	default:
		return invalid
	}
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.IndexFunc(config.Endpoint, unicode.IsSpace) >= 0 {
		return invalid
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	// The public OSMF service forbids generic place-search integrations.
	// Operators may attach their own Nominatim or a contracted endpoint.
	if host == "nominatim.openstreetmap.org" || strings.HasSuffix(host, ".nominatim.openstreetmap.org") {
		return invalid
	}
	if (config.Provider == "kakao" || config.Provider == "naver") && parsed.Scheme != "https" {
		return invalid
	}
	return nil
}
func applyAdministratorPlaceSearchInput(app core.App, next *storedAdministratorSettings, input *administratorPlaceSearchSettingsInput, current storedAdministratorSettings) error {
	next.PlaceSearchEnabled, next.PlaceSearchConfigCipher = current.PlaceSearchEnabled, current.PlaceSearchConfigCipher
	if input == nil {
		return nil
	}
	if input.ClearConfig {
		next.PlaceSearchEnabled = false
		next.PlaceSearchConfigCipher = ""
		return nil
	}
	config, err := storedPlaceSearchConfig(app, current)
	if err != nil {
		return err
	}
	previous := config
	input.Provider = strings.TrimSpace(input.Provider)
	if input.Provider != config.Provider {
		config = placeSearchConfig{Provider: input.Provider}
	}
	if value := strings.TrimSpace(input.Endpoint); value != "" {
		config.Endpoint = value
	}
	if value := strings.TrimSpace(input.APIKey); value != "" {
		config.APIKey = value
	}
	if value := strings.TrimSpace(input.APIKeyID); value != "" {
		config.APIKeyID = value
	}
	next.PlaceSearchEnabled = input.Enabled
	if !input.Enabled && config == (placeSearchConfig{}) {
		return nil
	}
	if err = normalizePlaceSearchConfig(&config); err != nil {
		return err
	}
	if config == previous {
		return nil
	}
	plain, err := json.Marshal(config)
	if err != nil {
		return err
	}
	next.PlaceSearchConfigCipher, err = encryptAdministratorSetting(app, cipherServerSettings, string(plain))
	return err
}
