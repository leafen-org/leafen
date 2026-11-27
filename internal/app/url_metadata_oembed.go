package app

import (
	"context"
	"encoding/json"
	"image"
	_ "image/gif"  // a video site's still may be a GIF (Loom's are)
	_ "image/jpeg" // or a JPEG
	_ "image/png"
	"io"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"
)

// An oEmbed answer is a few hundred bytes; a still's size is in its first bytes.
const (
	urlMetadataOEmbedMaxBytes    = 64 * 1024
	urlMetadataStillHeaderBytes  = 1024 * 1024
	urlMetadataMaxVideoStillSide = 16384
)

// readOEmbedVideoSize is a video's own shape: the size of the still its
// page's oEmbed answer names, read through the same guarded client as the page.
// Notion draws a Loom video so (seen 2026-11-06: stills of 1024×640 and
// 1280×720 drawn 704×440 and 704×396), not in oEmbed's player box, which is
// 1152×864 for both and puts bars over and under the picture. A still it
// can't read leaves the player box, as Notion then draws (seen 2026-11-06
// later: the GIF still's video drawn 704×528). Anything else, or a failed
// answer, gives no size: the page's other facts still stand.
func (fetcher *urlMetadataFetcher) readOEmbedVideoSize(ctx context.Context, address string) (int, int) {
	body, ok := fetcher.readOEmbedPart(ctx, address, "application/json")
	if !ok {
		return 0, 0
	}
	raw, err := io.ReadAll(io.LimitReader(body, urlMetadataOEmbedMaxBytes+1))
	_ = body.Close()
	if err != nil || len(raw) > urlMetadataOEmbedMaxBytes {
		return 0, 0
	}
	var answer struct {
		Type         string `json:"type"`
		ThumbnailURL string `json:"thumbnail_url"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
	}
	if json.Unmarshal(raw, &answer) != nil || !strings.EqualFold(answer.Type, "video") {
		return 0, 0
	}
	if width, height, ok := fetcher.readOEmbedStillSize(ctx, answer.ThumbnailURL, address); ok {
		return width, height
	}
	if videoSideFits(answer.Width) && videoSideFits(answer.Height) {
		return answer.Width, answer.Height
	}
	return 0, 0
}

func videoSideFits(side int) bool { return side >= 1 && side <= urlMetadataMaxVideoStillSide }

// readOEmbedStillSize is the size of the still an oEmbed answer names, from its first bytes.
func (fetcher *urlMetadataFetcher) readOEmbedStillSize(ctx context.Context, thumbnail, address string) (int, int, bool) {
	if thumbnail == "" {
		return 0, 0, false
	}
	still, ok := fetcher.readOEmbedPart(ctx, resolveURLMetadataIcon(thumbnail, address), "image/*")
	if !ok {
		return 0, 0, false
	}
	defer still.Close()
	config, _, err := image.DecodeConfig(io.LimitReader(still, urlMetadataStillHeaderBytes))
	if err != nil || !videoSideFits(config.Width) || !videoSideFits(config.Height) {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// readOEmbedPart opens one public address the oEmbed reading needs, with no redirects.
func (fetcher *urlMetadataFetcher) readOEmbedPart(ctx context.Context, address, accept string) (io.ReadCloser, bool) {
	target, err := normalizeURLMetadataURL(address)
	if err != nil {
		return nil, false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "LeafenBot/1.0")
	response, err := fetcher.client.Do(request)
	if err != nil {
		return nil, false
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, false
	}
	return response.Body, true
}
