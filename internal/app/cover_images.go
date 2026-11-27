package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Each server's operator supplies its own application key. Browsers only
// request this server, and pictures remain on Unsplash's image CDN. No key,
// provider response body or remote error is returned to a browser or logged.
const unsplashAccessKeyEnv = "LEAFEN_UNSPLASH_ACCESS_KEY"

var unsplashPhotoID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type unsplashCoverProvider struct {
	client *http.Client
	key    string
}

type unsplashPhoto struct {
	ID             string `json:"id"`
	AltDescription string `json:"alt_description"`
	URLs           struct {
		Regular string `json:"regular"`
		Small   string `json:"small"`
	} `json:"urls"`
	Links struct {
		DownloadLocation string `json:"download_location"`
	} `json:"links"`
	User struct {
		Name  string `json:"name"`
		Links struct {
			HTML string `json:"html"`
		} `json:"links"`
	} `json:"user"`
}

var (
	errCoverProviderUnavailable = errors.New("cover provider unavailable")
	errCoverProviderRateLimited = errors.New("cover provider rate limited")
	errCoverProviderFailed      = errors.New("cover provider request failed")
)

func newUnsplashCoverProvider() *unsplashCoverProvider {
	key := strings.TrimSpace(os.Getenv(unsplashAccessKeyEnv))
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`).MatchString(key) {
		key = ""
	}
	return &unsplashCoverProvider{key: key, client: &http.Client{
		Timeout:       7 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func unsplashCoverConfigured() bool { return newUnsplashCoverProvider().key != "" }

func (provider *unsplashCoverProvider) read(ctx context.Context, path string, query url.Values, output any) (http.Header, error) {
	if provider.key == "" {
		return nil, errCoverProviderUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.unsplash.com"+path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, errCoverProviderFailed
	}
	request.Header.Set("Authorization", "Client-ID "+provider.key)
	request.Header.Set("Accept-Version", "v1")
	request.Header.Set("Accept", "application/json")
	response, err := provider.client.Do(request)
	if err != nil {
		return nil, errCoverProviderFailed
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, errCoverProviderRateLimited
	}
	if response.StatusCode != http.StatusOK {
		return nil, errCoverProviderFailed
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil || len(body) > 2*1024*1024 || json.Unmarshal(body, output) != nil {
		return nil, errCoverProviderFailed
	}
	return response.Header, nil
}

func unsplashImageURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || len(value) > 2048 || parsed.Scheme != "https" || parsed.Host != "images.unsplash.com" ||
		parsed.User != nil || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/photo-") {
		return ""
	}
	return parsed.String()
}

func unsplashProfileURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || len(value) > 512 || parsed.Scheme != "https" || parsed.Host != "unsplash.com" || parsed.User != nil ||
		!regexp.MustCompile(`^/@[A-Za-z0-9_-]+$`).MatchString(parsed.Path) {
		return ""
	}
	parsed.Fragment = ""
	query := url.Values{"utm_source": {"leafen"}, "utm_medium": {"referral"}}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func projectUnsplashPhoto(photo unsplashPhoto) (coverImagePhoto, bool) {
	image, thumb := unsplashImageURL(photo.URLs.Regular), unsplashImageURL(photo.URLs.Small)
	profile, author := unsplashProfileURL(photo.User.Links.HTML), strings.TrimSpace(photo.User.Name)
	if !unsplashPhotoID.MatchString(photo.ID) || image == "" || thumb == "" || profile == "" || author == "" || len([]rune(author)) > 120 || strings.IndexFunc(author, unicode.IsControl) >= 0 {
		return coverImagePhoto{}, false
	}
	alt := []rune(strings.TrimSpace(photo.AltDescription))
	if len(alt) > 200 {
		alt = alt[:200]
	}
	return coverImagePhoto{ID: photo.ID, ImageURL: image, ThumbnailURL: thumb,
		Author: author, ProfileURL: profile, Description: string(alt)}, true
}

func (provider *unsplashCoverProvider) search(ctx context.Context, text string, page int) (coverImageSearchResponse, error) {
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"30"}}
	photos := []unsplashPhoto{}
	var next int
	if text == "" {
		header, err := provider.read(ctx, "/photos", query, &photos)
		if err != nil {
			return coverImageSearchResponse{}, err
		}
		if strings.Contains(header.Get("Link"), `rel="next"`) {
			next = page + 1
		}
	} else {
		query.Set("query", text)
		query.Set("orientation", "landscape")
		query.Set("content_filter", "high")
		result := struct {
			Results    []unsplashPhoto `json:"results"`
			TotalPages int             `json:"total_pages"`
		}{}
		if _, err := provider.read(ctx, "/search/photos", query, &result); err != nil {
			return coverImageSearchResponse{}, err
		}
		photos = result.Results
		if page < result.TotalPages {
			next = page + 1
		}
	}
	if len(photos) > 30 {
		return coverImageSearchResponse{}, errCoverProviderFailed
	}
	output := coverImageSearchResponse{Photos: []coverImagePhoto{}, NextPage: next}
	for _, photo := range photos {
		if projected, valid := projectUnsplashPhoto(photo); valid {
			output.Photos = append(output.Photos, projected)
		}
	}
	return output, nil
}

func (provider *unsplashCoverProvider) selectPhoto(ctx context.Context, id string) (coverImageSelectionResponse, error) {
	photo := unsplashPhoto{}
	if _, err := provider.read(ctx, "/photos/"+id, nil, &photo); err != nil {
		return coverImageSelectionResponse{}, err
	}
	projected, valid := projectUnsplashPhoto(photo)
	download, err := url.Parse(photo.Links.DownloadLocation)
	if !valid || photo.ID != id || err != nil || download.Scheme != "https" || download.Host != "api.unsplash.com" ||
		download.User != nil || download.Fragment != "" || download.Path != "/photos/"+id+"/download" {
		return coverImageSelectionResponse{}, errCoverProviderFailed
	}
	var notification map[string]any
	if _, err := provider.read(ctx, download.Path, download.Query(), &notification); err != nil {
		return coverImageSelectionResponse{}, err
	}
	// A URL fragment is never sent to the image CDN. It keeps public credit
	// with this one cover value through history, copying, export and import,
	// rather than introducing a second presentation record or fetching credit
	// every time somebody reads a page. The image's API query, including ixid,
	// remains untouched. Readers validate this versioned fragment and its
	// hosts (web lib/coverCredit.ts) and name the photographer and Unsplash on
	// the picture while the pointer is over it.
	fragment := url.Values{"leafen-unsplash": {"1"}, "author": {projected.Author}, "profile": {projected.ProfileURL}}.Encode()
	return coverImageSelectionResponse{Cover: projected.ImageURL + "#" + fragment}, nil
}
