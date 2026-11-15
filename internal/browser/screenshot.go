package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
)

// Screenshot limits (ADR-0061 section 7).
const (
	MaxShotLongSide = 1568
	MaxShotBytes    = 1_500_000 // 1.5 MB
	maxShotPixels   = 4096      // a sanity bound on what the browser hands back
	passwordPadPx   = 4
)

// maxShotBytes is the cap in use (a var so a test can lower it).
var maxShotBytes = MaxShotBytes

// Shot is a viewport screenshot, already redacted and size-capped.
type Shot struct {
	Data   []byte
	Mime   string
	Width  int
	Height int
}

// jpegQualities are tried in order until the image fits the byte cap.
var jpegQualities = []int{80, 70, 60, 50, 40}

// Screenshot captures the VIEWPORT of a tab (never the window or the screen),
// scaled so its long side is at most 1,568 px, with every password input
// painted over by the daemon (not by the page: page script can neither remove
// nor skip the redaction) and encoded as a JPEG of at most 1.5 MB.
func (s *Session) Screenshot(ctx context.Context, tabID string) (Shot, error) {
	ctx, cancel, err := s.begin(ctx)
	if err != nil {
		return Shot{}, err
	}
	defer cancel()
	tab, err := s.tabFor(tabID)
	if err != nil {
		return Shot{}, err
	}

	// Which regions must be hidden: found BEFORE the capture and fail closed,
	// so a screenshot is never taken without knowing where the password fields are.
	boxes, err := s.passwordBoxes(ctx, tab)
	if err != nil {
		return Shot{}, fmt.Errorf("browser: screenshot refused, password fields could not be located: %w", err)
	}

	var lm struct {
		CSSVisualViewport struct {
			PageX        float64 `json:"pageX"`
			PageY        float64 `json:"pageY"`
			ClientWidth  float64 `json:"clientWidth"`
			ClientHeight float64 `json:"clientHeight"`
		} `json:"cssVisualViewport"`
	}
	if err := s.tabCall(ctx, tab, "Page.getLayoutMetrics", nil, &lm); err != nil {
		return Shot{}, err
	}
	vp := lm.CSSVisualViewport
	if vp.ClientWidth < 1 || vp.ClientHeight < 1 {
		return Shot{}, errors.New("browser: the page has no visible viewport")
	}
	scale := math.Min(1, float64(MaxShotLongSide)/math.Max(vp.ClientWidth, vp.ClientHeight))

	capture := func(scale float64) (image.Image, error) {
		var shot struct {
			Data string `json:"data"`
		}
		err := s.tabCall(ctx, tab, "Page.captureScreenshot", map[string]any{
			"format": "png",
			"clip": map[string]any{
				"x": vp.PageX, "y": vp.PageY, "width": vp.ClientWidth, "height": vp.ClientHeight, "scale": scale,
			},
			"captureBeyondViewport": false,
		}, &shot)
		if err != nil {
			return nil, err
		}
		raw, err := base64.StdEncoding.DecodeString(shot.Data)
		if err != nil {
			return nil, errors.New("browser: the browser returned an unreadable screenshot")
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxShotPixels || cfg.Height > maxShotPixels {
			return nil, errors.New("browser: the browser returned a screenshot of unusable size")
		}
		src, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, errors.New("browser: the browser returned an unreadable screenshot")
		}
		return src, nil
	}
	src, err := capture(scale)
	if err != nil {
		return Shot{}, err
	}
	// The image is the viewport times the clip scale times the device pixel
	// ratio. On a display with a ratio above 1 it is larger than assumed: capture
	// again at the corrected scale. If the size is still not what was asked for,
	// refuse: the redaction rectangles could not be placed with confidence.
	target := scale // the size the image must have: viewport times this
	sizeRatio := func(im image.Image) (float64, float64) {
		return float64(im.Bounds().Dx()) / (vp.ClientWidth * target), float64(im.Bounds().Dy()) / (vp.ClientHeight * target)
	}
	if rw, rh := sizeRatio(src); rw > 1.02 && math.Abs(rw-rh) < 0.02 {
		if src, err = capture(target / rw); err != nil {
			return Shot{}, err
		}
	}
	if rw, rh := sizeRatio(src); math.Abs(rw-1) > 0.02 || math.Abs(rh-1) > 0.02 {
		return Shot{}, errors.New("browser: screenshot refused, the captured size is not the requested viewport size")
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)

	// Quads are in viewport CSS pixels; the image is the viewport scaled to
	// whatever size was actually delivered.
	sx, sy := float64(img.Bounds().Dx())/vp.ClientWidth, float64(img.Bounds().Dy())/vp.ClientHeight
	for _, q := range boxes {
		r := image.Rect(
			int(math.Floor(q.minX*sx))-passwordPadPx, int(math.Floor(q.minY*sy))-passwordPadPx,
			int(math.Ceil(q.maxX*sx))+passwordPadPx, int(math.Ceil(q.maxY*sy))+passwordPadPx,
		).Intersect(img.Bounds())
		if !r.Empty() {
			draw.Draw(img, r, &image.Uniform{color.RGBA{0x80, 0x80, 0x80, 0xff}}, image.Point{}, draw.Src)
		}
	}

	for _, q := range jpegQualities {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return Shot{}, err
		}
		if buf.Len() <= maxShotBytes {
			return Shot{Data: buf.Bytes(), Mime: "image/jpeg", Width: img.Bounds().Dx(), Height: img.Bounds().Dy()}, nil
		}
	}
	return Shot{}, fmt.Errorf("browser: the screenshot is larger than %d bytes even at the lowest quality; use browser_snapshot", maxShotBytes)
}

type box struct{ minX, minY, maxX, maxY float64 }

// passwordBoxes returns the viewport-relative bounding boxes of every
// password input (the first 100), via the DOM domain: no page script runs.
func (s *Session) passwordBoxes(ctx context.Context, tab *tabState) ([]box, error) {
	var doc struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := s.tabCall(ctx, tab, "DOM.getDocument", map[string]any{"depth": 0}, &doc); err != nil {
		return nil, err
	}
	var q struct {
		NodeIDs []int64 `json:"nodeIds"`
	}
	if err := s.tabCall(ctx, tab, "DOM.querySelectorAll", map[string]any{"nodeId": doc.Root.NodeID, "selector": "input[type=password i]"}, &q); err != nil {
		return nil, err
	}
	if len(q.NodeIDs) > maxPasswordScan {
		// A page can add decoy inputs ahead of the real one: never cover only some.
		return nil, fmt.Errorf("the page has more than %d password inputs", maxPasswordScan)
	}
	var out []box
	for _, id := range q.NodeIDs {
		var bm struct {
			Model struct {
				Border []float64 `json:"border"`
			} `json:"model"`
		}
		if err := s.tabCall(ctx, tab, "DOM.getBoxModel", map[string]any{"nodeId": id}, &bm); err != nil {
			// An input with no box (display:none) shows nothing: nothing to hide.
			// Any OTHER failure could be a visible field: fail closed.
			var ce *CDPError
			if errors.As(err, &ce) && strings.Contains(ce.Message, "box model") {
				continue
			}
			return nil, err
		}
		b := bm.Model.Border
		if len(b) != 8 {
			continue
		}
		bx := box{minX: b[0], minY: b[1], maxX: b[0], maxY: b[1]}
		for i := 2; i < 8; i += 2 {
			bx.minX, bx.maxX = math.Min(bx.minX, b[i]), math.Max(bx.maxX, b[i])
			bx.minY, bx.maxY = math.Min(bx.minY, b[i+1]), math.Max(bx.maxY, b[i+1])
		}
		out = append(out, bx)
	}
	return out, nil
}
