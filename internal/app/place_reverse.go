package app

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

func validPlacePoint(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) &&
		lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// reverse names a point with the provider an administrator chose, as a
// map's pin names the page it makes (Notion's, seen 2026-11-02: "Decatur,
// KS, 미국"): its locality, region and country, and its address. A point the
// provider can't name has none. It keeps the search's pace per server and
// its transient cache; the endpoint and keys never leave the server.
func (provider *placeSearchProvider) reverse(ctx context.Context, config placeSearchConfig, lat, lon float64, language string) (placeReverseResponse, error) {
	language = placeAcceptLanguage(language)
	point := strconv.FormatFloat(lat, 'f', 6, 64) + "," + strconv.FormatFloat(lon, 'f', 6, 64)
	cacheKey := automationDigest("reverse\x00" + config.Provider + "\x00" + config.Endpoint + "\x00" + config.APIKey + "\x00" + config.APIKeyID + "\x00" + language + "\x00" + point)
	now := time.Now()
	provider.mutex.Lock()
	if entry, found := provider.cache[cacheKey]; found && now.Before(entry.expires) {
		provider.mutex.Unlock()
		if len(entry.value.Items) == 0 {
			return placeReverseResponse{}, nil
		}
		return placeReverseResponse{Name: entry.value.Items[0].Name, Address: entry.value.Items[0].Address}, nil
	}
	if now.Before(provider.next) {
		provider.mutex.Unlock()
		return placeReverseResponse{}, errPlaceSearchRateLimited
	}
	provider.next = now.Add(time.Second)
	provider.mutex.Unlock()
	endpoint, err := placeReverseEndpoint(config)
	if err != nil {
		return placeReverseResponse{}, err
	}
	params := endpoint.Query()
	latText, lonText := strconv.FormatFloat(lat, 'f', -1, 64), strconv.FormatFloat(lon, 'f', -1, 64)
	switch config.Provider {
	case "nominatim":
		params.Set("lat", latText)
		params.Set("lon", lonText)
		params.Set("format", "jsonv2")
		params.Set("zoom", "10")
		params.Set("addressdetails", "1")
		if language != "" {
			params.Set("accept-language", language)
		}
	case "photon":
		params.Set("lat", latText)
		params.Set("lon", lonText)
		params.Set("limit", "1")
	case "kakao":
		params.Set("x", lonText)
		params.Set("y", latText)
	case "naver":
		params.Set("coords", lonText+","+latText)
		params.Set("output", "json")
		params.Set("orders", "admcode,addr,roadaddr")
	}
	endpoint.RawQuery = params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return placeReverseResponse{}, errPlaceSearchFailed
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Leafen place search (+https://github.com/leafen-org/leafen)")
	if language != "" {
		request.Header.Set("Accept-Language", language)
	}
	if config.Provider == "kakao" {
		request.Header.Set("Authorization", "KakaoAK "+config.APIKey)
	}
	if config.Provider == "naver" {
		request.Header.Set("x-ncp-apigw-api-key-id", config.APIKeyID)
		request.Header.Set("x-ncp-apigw-api-key", config.APIKey)
	}
	response, err := provider.client.Do(request)
	if err != nil {
		return placeReverseResponse{}, errPlaceSearchFailed
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return placeReverseResponse{}, errPlaceSearchRateLimited
	}
	if response.StatusCode != http.StatusOK {
		return placeReverseResponse{}, errPlaceSearchFailed
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return placeReverseResponse{}, errPlaceSearchFailed
	}
	output, err := parsePlaceReverseResponse(config, raw)
	if err != nil {
		return placeReverseResponse{}, errPlaceSearchFailed
	}
	cached := placeSearchResponse{Items: []placeSearchItem{}}
	if output.Name != "" || output.Address != "" {
		cached.Items = append(cached.Items, placeSearchItem{Lat: lat, Lon: lon, Name: output.Name, Address: output.Address})
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if len(provider.cache) >= 128 {
		for key := range provider.cache {
			delete(provider.cache, key)
			break
		}
	}
	provider.cache[cacheKey] = cachedPlaceSearch{value: cached, expires: now.Add(time.Hour)}
	return output, nil
}

// placeReverseEndpoint is the provider's reverse lookup on the host the
// administrator's search endpoint names: Nominatim's and Photon's "reverse"
// beside their search, Kakao's coord2address, NAVER's reverse geocoding.
func placeReverseEndpoint(config placeSearchConfig) (*url.URL, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, failedBecause(errPlaceSearchUnavailable, err)
	}
	switch config.Provider {
	case "nominatim", "photon":
		dir, last := path.Split(strings.TrimSuffix(endpoint.Path, "/"))
		name := "reverse"
		if strings.HasSuffix(last, ".php") {
			name = "reverse.php"
		}
		if dir == "" {
			dir = "/"
		}
		endpoint.Path = path.Join(dir, name)
	case "kakao":
		endpoint.Path = "/v2/local/geo/coord2address.json"
		endpoint.RawQuery = ""
	case "naver":
		endpoint.Path = "/map-reversegeocode/v2/gc"
		endpoint.RawQuery = ""
	default:
		return nil, errPlaceSearchUnavailable
	}
	return endpoint, nil
}

// placeAcceptLanguage keeps a browser's language list as a provider reads it.
func placeAcceptLanguage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 64 {
		value = value[:64]
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(",;=.- *", r)) {
			return ""
		}
	}
	return value
}

func placeNameParts(parts ...string) string {
	kept := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && (len(kept) == 0 || kept[len(kept)-1] != part) {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ", ")
}

func parsePlaceReverseResponse(config placeSearchConfig, raw []byte) (placeReverseResponse, error) {
	var name, address string
	switch config.Provider {
	case "nominatim":
		var data struct {
			Error       string `json:"error"`
			DisplayName string `json:"display_name"`
			Address     map[string]string
		}
		if json.Unmarshal(raw, &data) != nil {
			return placeReverseResponse{}, errPlaceSearchFailed
		}
		if data.Error != "" {
			return placeReverseResponse{}, nil
		}
		locality := ""
		for _, key := range []string{"city", "town", "village", "hamlet", "municipality", "county"} {
			if locality = data.Address[key]; locality != "" {
				break
			}
		}
		// A region's short code where it has one ("US-KS" → "KS"), as Notion names it.
		region := data.Address["state"]
		if code := data.Address["ISO3166-2-lvl4"]; code != "" {
			if _, short, found := strings.Cut(code, "-"); found && len(short) <= 3 && strings.Trim(short, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == "" {
				region = short
			}
		}
		name, address = placeNameParts(locality, region, data.Address["country"]), data.DisplayName
	case "photon":
		var data struct {
			Features []struct {
				Properties struct {
					Name, City, County, State, Country, Street, Postcode string
					HouseNumber                                          string `json:"housenumber"`
				} `json:"properties"`
			} `json:"features"`
		}
		if json.Unmarshal(raw, &data) != nil {
			return placeReverseResponse{}, errPlaceSearchFailed
		}
		if len(data.Features) == 0 {
			return placeReverseResponse{}, nil
		}
		item := data.Features[0].Properties
		locality := item.City
		if locality == "" {
			locality = item.County
		}
		name = placeNameParts(locality, item.State, item.Country)
		address = placeNameParts(item.Street+" "+item.HouseNumber, item.City, item.Postcode, item.Country)
	case "kakao":
		var data struct {
			Documents []struct {
				Address *struct {
					Name    string `json:"address_name"`
					Region1 string `json:"region_1depth_name"`
					Region2 string `json:"region_2depth_name"`
					Region3 string `json:"region_3depth_name"`
				} `json:"address"`
				Road *struct {
					Name string `json:"address_name"`
				} `json:"road_address"`
			} `json:"documents"`
		}
		if json.Unmarshal(raw, &data) != nil {
			return placeReverseResponse{}, errPlaceSearchFailed
		}
		if len(data.Documents) == 0 || data.Documents[0].Address == nil {
			return placeReverseResponse{}, nil
		}
		item := data.Documents[0]
		name = placeNameParts(item.Address.Region3, item.Address.Region2, item.Address.Region1)
		address = item.Address.Name
		if item.Road != nil && item.Road.Name != "" {
			address = item.Road.Name
		}
	case "naver":
		var data struct {
			Status struct {
				Code int `json:"code"`
			} `json:"status"`
			Results []struct {
				Region struct {
					Area1 struct{ Name string } `json:"area1"`
					Area2 struct{ Name string } `json:"area2"`
					Area3 struct{ Name string } `json:"area3"`
				} `json:"region"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &data) != nil {
			return placeReverseResponse{}, errPlaceSearchFailed
		}
		if len(data.Results) == 0 {
			return placeReverseResponse{}, nil
		}
		region := data.Results[0].Region
		name = placeNameParts(region.Area3.Name, region.Area2.Name, region.Area1.Name)
		address = strings.TrimSpace(region.Area1.Name + " " + region.Area2.Name + " " + region.Area3.Name)
	default:
		return placeReverseResponse{}, errPlaceSearchFailed
	}
	for _, secret := range []string{config.APIKey, config.APIKeyID, config.Endpoint} {
		if secret != "" && (strings.Contains(name, secret) || strings.Contains(address, secret)) {
			return placeReverseResponse{}, nil
		}
	}
	return placeReverseResponse{Name: placeSearchText(name, 256), Address: placeSearchText(address, 1024)}, nil
}
