package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	errPlaceSearchUnavailable = errors.New(errorCodePlaceSearchUnavailable)
	errPlaceSearchRateLimited = errors.New(errorCodePlaceSearchRateLimited)
	errPlaceSearchFailed      = errors.New(errorCodePlaceSearchFailed)
)

const placeSearchResultLimit = 10

type cachedPlaceSearch struct {
	value   placeSearchResponse
	expires time.Time
}
type placeSearchProvider struct {
	client *http.Client
	mutex  sync.Mutex
	next   time.Time
	cache  map[string]cachedPlaceSearch
}

func newPlaceSearchProvider() *placeSearchProvider {
	return &placeSearchProvider{client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, cache: map[string]cachedPlaceSearch{}}
}

// Per-server rate admission covers all users. Only a deliberate button press
// reaches this bounded upstream request; endpoint/keys never cross the route.
func (provider *placeSearchProvider) search(ctx context.Context, config placeSearchConfig, query string) (placeSearchResponse, error) {
	cacheKey := automationDigest(config.Provider + "\x00" + config.Endpoint + "\x00" + config.APIKey + "\x00" + config.APIKeyID + "\x00" + query)
	now := time.Now()
	provider.mutex.Lock()
	if entry, found := provider.cache[cacheKey]; found && now.Before(entry.expires) {
		provider.mutex.Unlock()
		return entry.value, nil
	}
	if now.Before(provider.next) {
		provider.mutex.Unlock()
		return placeSearchResponse{}, errPlaceSearchRateLimited
	}
	provider.next = now.Add(time.Second)
	provider.mutex.Unlock()
	endpoint, _ := url.Parse(config.Endpoint)
	params := endpoint.Query()
	switch config.Provider {
	case "nominatim":
		params.Set("q", query)
		params.Set("format", "jsonv2")
		params.Set("limit", "10")
	case "photon":
		params.Set("q", query)
		params.Set("limit", "10")
	case "kakao":
		params.Set("query", query)
		params.Set("size", "10")
	case "naver":
		params.Set("query", query)
		params.Set("count", "10")
	default:
		return placeSearchResponse{}, errPlaceSearchUnavailable
	}
	endpoint.RawQuery = params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return placeSearchResponse{}, errPlaceSearchFailed
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Leafen place search (+https://github.com/leafen-org/leafen)")
	if config.Provider == "kakao" {
		request.Header.Set("Authorization", "KakaoAK "+config.APIKey)
	}
	if config.Provider == "naver" {
		request.Header.Set("x-ncp-apigw-api-key-id", config.APIKeyID)
		request.Header.Set("x-ncp-apigw-api-key", config.APIKey)
	}
	response, err := provider.client.Do(request)
	if err != nil {
		return placeSearchResponse{}, errPlaceSearchFailed
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return placeSearchResponse{}, errPlaceSearchRateLimited
	}
	if response.StatusCode != http.StatusOK {
		return placeSearchResponse{}, errPlaceSearchFailed
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return placeSearchResponse{}, errPlaceSearchFailed
	}
	output, err := parsePlaceSearchResponse(config, raw)
	if err != nil {
		return placeSearchResponse{}, errPlaceSearchFailed
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for key, entry := range provider.cache {
		if !now.Before(entry.expires) {
			delete(provider.cache, key)
		}
	}
	// Searches and results are transient and bounded; no addresses enter logs
	// or a second persistent database. Configuration changes have a new key.
	if len(provider.cache) >= 128 {
		for key := range provider.cache {
			delete(provider.cache, key)
			break
		}
	}
	provider.cache[cacheKey] = cachedPlaceSearch{value: output, expires: now.Add(time.Hour)}
	return output, nil
}

func placeSearchText(value string, limit int) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value))
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
func parsePlaceSearchResponse(config placeSearchConfig, raw []byte) (placeSearchResponse, error) {
	output := placeSearchResponse{Items: []placeSearchItem{}}
	add := func(lat, lon float64, name, address string) {
		if len(output.Items) >= placeSearchResultLimit || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return
		}
		for _, secret := range []string{config.APIKey, config.APIKeyID, config.Endpoint} {
			if secret != "" && (strings.Contains(name, secret) || strings.Contains(address, secret)) {
				return
			}
		}
		output.Items = append(output.Items, placeSearchItem{Lat: lat, Lon: lon, Name: placeSearchText(name, 256), Address: placeSearchText(address, 1024)})
	}
	addStrings := func(lat, lon, name, address string) {
		a, e1 := strconv.ParseFloat(lat, 64)
		b, e2 := strconv.ParseFloat(lon, 64)
		if e1 == nil && e2 == nil {
			add(a, b, name, address)
		}
	}
	switch config.Provider {
	case "nominatim":
		var items []struct {
			Lat         string `json:"lat"`
			Lon         string `json:"lon"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		}
		if json.Unmarshal(raw, &items) != nil || items == nil {
			return output, errPlaceSearchFailed
		}
		for _, item := range items {
			name := item.Name
			if name == "" {
				name = strings.SplitN(item.DisplayName, ",", 2)[0]
			}
			addStrings(item.Lat, item.Lon, name, item.DisplayName)
		}
	case "photon":
		var data struct {
			Features []struct {
				Geometry struct {
					Type        string    `json:"type"`
					Coordinates []float64 `json:"coordinates"`
				} `json:"geometry"`
				Properties struct {
					Name, Street, City, Postcode, Country string
					HouseNumber                           string `json:"housenumber"`
				} `json:"properties"`
			} `json:"features"`
		}
		if json.Unmarshal(raw, &data) != nil || data.Features == nil {
			return output, errPlaceSearchFailed
		}
		for _, item := range data.Features {
			if item.Geometry.Type != "Point" || len(item.Geometry.Coordinates) != 2 {
				continue
			}
			parts := []string{}
			for _, part := range []string{item.Properties.Street, item.Properties.HouseNumber, item.Properties.City, item.Properties.Postcode, item.Properties.Country} {
				if part != "" {
					parts = append(parts, part)
				}
			}
			add(item.Geometry.Coordinates[1], item.Geometry.Coordinates[0], item.Properties.Name, strings.Join(parts, ", "))
		}
	case "kakao":
		var data struct {
			Documents []struct {
				X, Y    string
				Name    string `json:"place_name"`
				Address string `json:"address_name"`
				Road    string `json:"road_address_name"`
			} `json:"documents"`
		}
		if json.Unmarshal(raw, &data) != nil || data.Documents == nil {
			return output, errPlaceSearchFailed
		}
		for _, item := range data.Documents {
			address := item.Road
			if address == "" {
				address = item.Address
			}
			addStrings(item.Y, item.X, item.Name, address)
		}
	case "naver":
		var data struct {
			Status    string `json:"status"`
			Addresses []struct {
				X, Y  string
				Road  string `json:"roadAddress"`
				Jibun string `json:"jibunAddress"`
			} `json:"addresses"`
		}
		if json.Unmarshal(raw, &data) != nil || data.Status != "OK" || data.Addresses == nil {
			return output, errPlaceSearchFailed
		}
		for _, item := range data.Addresses {
			address := item.Road
			if address == "" {
				address = item.Jibun
			}
			addStrings(item.Y, item.X, address, address)
		}
	default:
		return output, errPlaceSearchFailed
	}
	switch config.Provider {
	case "nominatim", "photon":
		output.Attribution = "© OpenStreetMap contributors"
		output.AttributionURL = "https://www.openstreetmap.org/copyright"
	case "kakao":
		output.Attribution = "Kakao"
		output.AttributionURL = "https://map.kakao.com/"
	case "naver":
		output.Attribution = "NAVER"
		output.AttributionURL = "https://map.naver.com/"
	}
	return output, nil
}
