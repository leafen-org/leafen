package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/spf13/cast"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"
)

// An uploaded photo is sent at the size the screen shows it, not as the
// several megabytes a phone takes (2026-10-17: pages and cards load faster
// and use less data, above all on phones and through a relay). A page, card,
// cover or published page asks for a width (?w=512, 1024 or 2048, the sizes
// Notion's own image server gives, seen 2026-10-17); the server makes that
// size from the original the first time it is asked, keeps it beside the
// original and gives the kept one after. Opening, downloading and
// agents read the original, which they never ask a width of. Only JPEG and
// PNG are made smaller: a GIF may move, an SVG is drawn and a WebP would come
// back as a larger PNG. An image no wider than the width goes as it is. The
// preview is given out under the original's own permission.

var productFilePreviewWidths = map[int]bool{512: true, 1024: true, 2048: true}

// productFilePreviewFolderPrefix starts the folder beside a stored file that
// holds its previews (PocketBase's own thumbnail folder name), which backups
// leave out.
const productFilePreviewFolderPrefix = "thumbs_"

// Making a preview decodes the whole photo, so it is bounded (2026-10-18): a
// small file can say it is 30,000 pixels square and take gigabytes to
// decode.
const (
	// No preview is shown of an image with more pixels, as Notion shows
	// none (seen 2026-10-18: a 200-megapixel photo comes 2,048 wide, a
	// 900-megapixel one is refused with 422 and the page says the image
	// couldn't be loaded); 16,383 pixels square is the limit image resizers
	// keep, or PocketBase's own where that is lower
	// (productFilePreviewPixelLimit). The original still opens and downloads.
	productFilePreviewMaxPixels = 16_383 * 16_383
	// PocketBase's thumbnails refuse an image with more pixels than this
	// (since v0.40.6), unless PB_THUMBS_MAX_SOURCE_PIXELS says otherwise.
	pocketBaseThumbMaxPixels = 250 << 20
	// A preview waits this long for memory others' previews hold, then the
	// original goes, kept by no cache, and a later look asks again.
	productFilePreviewWait = 20 * time.Second
	// Kept in the preview folder of an image no preview is made of — too
	// many pixels, or not a readable image — so it isn't decoded again.
	productFilePreviewNoneName = "none"
	// Kept beside it for an image with too many pixels, which is refused.
	productFilePreviewRefusedName = "refused"
)

// What is served for a preview asked for.
type productFilePreviewOutcome int

const (
	// The original, as for a width the image isn't wider than.
	productFilePreviewOriginal productFilePreviewOutcome = iota
	// The original, kept by no cache, as previews held the memory too long.
	productFilePreviewBusy
	// Nothing: the image has too many pixels to show.
	productFilePreviewRefused
)

// productFilePreviewMemory is the memory previews being made may hold at
// once, a third of what the server may use, and the previews being made
// now by the key they are kept under.
var productFilePreviewMemory = struct {
	once   sync.Once
	budget int64
	held   *semaphore.Weighted
	making singleflight.Group
}{}

func productFilePreviewBudget() (*semaphore.Weighted, int64) {
	memory := &productFilePreviewMemory
	memory.once.Do(func() {
		limit, _ := serverMemoryLimit()
		memory.budget = max(limit/3, 64*memoryMiB)
		memory.held = semaphore.NewWeighted(memory.budget)
	})
	return memory.held, memory.budget
}

var errProductFilePreviewBusy = errors.New("previews being made held the memory too long")

// productFilePreviewCost is the most memory making a preview of the image
// takes, in bytes: the decoded image, a turned copy of it, and the image
// narrowed to the preview's width before it is made shorter. A JPEG holds a
// byte for each of its samples (one a pixel for grey, 1.5 for the usual
// colour, three in full colour, four for CMYK) and four more a sample while
// a progressive one is read; a PNG four a pixel, eight in 16 bits. A copy
// its EXIF turn makes takes four a pixel. A JPEG whose header can't be read
// is taken at its worst.
func productFilePreviewCost(config image.Config, format string, header []byte, width int) int64 {
	pixels := float64(config.Width) * float64(config.Height)
	perPixel := 4.0
	switch {
	case format == "jpeg":
		layout, ok := readJPEGLayout(header)
		if !ok {
			layout = jpegLayout{progressive: true, samples: 4, turned: true}
		}
		perPixel = layout.samples
		if layout.progressive {
			perPixel += 4 * layout.samples
		}
		if layout.turned {
			perPixel += 4
		}
	case config.ColorModel == color.RGBA64Model, config.ColorModel == color.NRGBA64Model, config.ColorModel == color.Gray16Model:
		perPixel = 8
	}
	return int64(pixels*perPixel) + int64(width)*int64(max(config.Width, config.Height))*4
}

// jpegLayout is what decoding a JPEG holds: whether it is progressive, its
// samples a pixel, and whether its EXIF turns or flips it.
type jpegLayout struct {
	progressive bool
	samples     float64
	turned      bool
}

// readJPEGLayout reads a JPEG's EXIF and frame header from the bytes its
// size was read from.
func readJPEGLayout(header []byte) (jpegLayout, bool) {
	layout := jpegLayout{}
	if len(header) < 2 || header[0] != 0xFF || header[1] != 0xD8 {
		return layout, false
	}
	for at := 2; at+4 <= len(header); {
		if header[at] != 0xFF {
			return layout, false
		}
		marker := header[at+1]
		switch {
		case marker == 0xFF:
			at++
			continue
		case marker == 0x01, marker >= 0xD0 && marker <= 0xD8:
			at += 2
			continue
		}
		length := int(header[at+2])<<8 | int(header[at+3])
		segment := header[at+4 : min(at+2+length, len(header))]
		switch {
		case marker == 0xE1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")):
			layout.turned = exifOrientation(segment[6:]) > 1
		case marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC:
			// Precision, height, width, the number of components, then each
			// component's id, sampling (horizontal, vertical) and table.
			if len(segment) < 6 || segment[5] == 0 || len(segment) < 6+3*int(segment[5]) {
				return layout, false
			}
			components := int(segment[5])
			most := [2]int{1, 1}
			for index := range components {
				sampling := segment[6+3*index+1]
				most = [2]int{max(most[0], int(sampling>>4)), max(most[1], int(sampling&15))}
			}
			for index := range components {
				sampling := segment[6+3*index+1]
				layout.samples += float64(int(sampling>>4)*int(sampling&15)) / float64(most[0]*most[1])
			}
			layout.progressive = marker == 0xC2 || marker == 0xC6 || marker == 0xCA || marker == 0xCE
			return layout, true
		}
		at += 2 + length
	}
	return layout, false
}

// exifOrientation is the orientation an EXIF block's first directory names,
// or 0.
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	directory := int(order.Uint32(tiff[4:8]))
	if directory < 8 || directory+2 > len(tiff) {
		return 0
	}
	entries := int(order.Uint16(tiff[directory:]))
	for index := range entries {
		entry := directory + 2 + 12*index
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:]) == 0x0112 {
			return int(order.Uint16(tiff[entry+8:]))
		}
	}
	return 0
}

// productFilePreviewKey is the stored preview to serve for the width asked,
// made now if it isn't kept yet, or "" with what goes instead.
func productFilePreviewKey(ctx context.Context, fsys *filesystem.System, record *core.Record, rawWidth string) (string, productFilePreviewOutcome) {
	width, err := strconv.Atoi(rawWidth)
	if err != nil || !productFilePreviewWidths[width] {
		return "", productFilePreviewOriginal
	}
	if contentType := record.GetString("content_type"); contentType != "image/jpeg" && contentType != "image/png" {
		return "", productFilePreviewOriginal
	}
	blob := record.GetString("blob")
	original := record.BaseFilesPath() + "/" + blob
	folder := record.BaseFilesPath() + "/" + productFilePreviewFolderPrefix + blob + "/"
	// A new upload is a new blob name, so a changed file never meets an old preview.
	key := folder + strconv.Itoa(width) + "x0_" + blob
	if exists, err := fsys.Exists(key); err == nil && exists {
		return key, productFilePreviewOriginal
	}
	none, refused := folder+productFilePreviewNoneName, folder+productFilePreviewRefusedName
	if exists, err := fsys.Exists(none); err == nil && exists {
		if exists, err := fsys.Exists(refused); err == nil && exists {
			return "", productFilePreviewRefused
		}
		return "", productFilePreviewOriginal
	}
	reader, err := fsys.GetReader(original)
	if err != nil {
		return "", productFilePreviewOriginal
	}
	// What the size is read from holds a JPEG's EXIF and frame header too.
	var header bytes.Buffer
	config, format, err := image.DecodeConfig(io.TeeReader(reader, &header))
	reader.Close()
	if err != nil {
		if productFilePreviewUnreadable(err) {
			_ = fsys.Upload([]byte{}, none)
		}
		return "", productFilePreviewOriginal
	}
	if int64(config.Width)*int64(config.Height) > productFilePreviewPixelLimit() {
		_ = fsys.Upload([]byte{}, refused)
		_ = fsys.Upload([]byte{}, none)
		return "", productFilePreviewRefused
	}
	if config.Width <= width {
		return "", productFilePreviewOriginal
	}
	held, budget := productFilePreviewBudget()
	cost := productFilePreviewCost(config, format, header.Bytes(), width)
	if cost > budget {
		return "", productFilePreviewOriginal
	}
	// Asked for twice at once, the preview is made once.
	_, err, _ = productFilePreviewMemory.making.Do(key, func() (any, error) {
		waiting, stop := context.WithTimeout(context.WithoutCancel(ctx), productFilePreviewWait)
		defer stop()
		if held.Acquire(waiting, cost) != nil {
			return nil, errProductFilePreviewBusy
		}
		defer held.Release(cost)
		if exists, err := fsys.Exists(key); err == nil && exists {
			return nil, nil
		}
		// The preview keeps the photo's turn (EXIF) as PocketBase reads it.
		if err := fsys.CreateThumb(original, key, strconv.Itoa(width)+"x0"); err != nil {
			if productFilePreviewUnreadable(err) {
				_ = fsys.Upload([]byte{}, none)
			}
			return nil, err
		}
		return nil, nil
	})
	switch {
	case errors.Is(err, errProductFilePreviewBusy):
		return "", productFilePreviewBusy
	case err != nil:
		return "", productFilePreviewOriginal
	}
	return key, productFilePreviewOriginal
}

// refuseProductFilePreview answers a preview of an image with too many
// pixels to show.
func refuseProductFilePreview(event *core.RequestEvent) error {
	event.Response.Header().Del("Content-Type")
	event.Response.Header().Del("Content-Disposition")
	return apiError(event, http.StatusUnprocessableEntity, errorCodeFilePreviewTooLarge,
		"The image has too many pixels to show; open or download it instead.")
}

// productFilePreviewUnreadable is a file that isn't an image of its kind,
// as against storage that failed for a moment (a cut-off read is taken for
// the latter, as storage cuts reads off too).
func productFilePreviewUnreadable(err error) bool {
	var pngFormat png.FormatError
	var pngUnsupported png.UnsupportedError
	var jpegFormat jpeg.FormatError
	var jpegUnsupported jpeg.UnsupportedError
	return errors.Is(err, image.ErrFormat) || errors.As(err, &pngFormat) || errors.As(err, &pngUnsupported) ||
		errors.As(err, &jpegFormat) || errors.As(err, &jpegUnsupported)
}

// productFilePreviewPixelLimit is the most pixels a preview is made of: the
// lower of Leafen's limit and the one PocketBase's CreateThumb holds to, read
// as PocketBase reads it. An image between the two would pass here, be
// refused there, and go as the original at every look rather than be
// refused once.
func productFilePreviewPixelLimit() int64 {
	pocketBase := cast.ToInt64(os.Getenv("PB_THUMBS_MAX_SOURCE_PIXELS"))
	if pocketBase <= 0 {
		pocketBase = pocketBaseThumbMaxPixels
	}
	return min(productFilePreviewMaxPixels, pocketBase)
}

// deleteProductFilePreviews removes the previews made of a stored file, with
// the file: a deleted photo leaves no smaller copy behind.
func deleteProductFilePreviews(fsys *filesystem.System, record *core.Record) error {
	return errors.Join(fsys.DeletePrefix(record.BaseFilesPath() + "/" + productFilePreviewFolderPrefix + record.GetString("blob") + "/")...)
}
