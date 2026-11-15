package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"strings"
	"testing"
)

func solidPNG(t *testing.T, w, h int, c color.Color) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func noisePNG(t *testing.T, w, h int) string {
	t.Helper()
	r := rand.New(rand.NewSource(1))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(r.Intn(256))
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

type shotRig struct {
	vw, vh, px, py float64
	boxes          [][]float64 // border quads of the password inputs
	boxErr         string
	queryErr       string
	png            func(w, h int) string
}

func (r *shotRig) hook(f *fakeCDP, c fakeCall) (any, string) {
	switch c.Method {
	case "DOM.getDocument":
		return map[string]any{"root": map[string]any{"nodeId": 1}}, ""
	case "DOM.querySelectorAll":
		if r.queryErr != "" {
			return nil, r.queryErr
		}
		ids := []int{}
		for i := range r.boxes {
			ids = append(ids, 10+i)
		}
		return map[string]any{"nodeIds": ids}, ""
	case "DOM.getBoxModel":
		if r.boxErr != "" {
			return nil, r.boxErr
		}
		i := int(c.Params["nodeId"].(float64)) - 10
		return map[string]any{"model": map[string]any{"border": r.boxes[i]}}, ""
	case "Page.getLayoutMetrics":
		return map[string]any{"cssVisualViewport": map[string]any{"pageX": r.px, "pageY": r.py, "clientWidth": r.vw, "clientHeight": r.vh}}, ""
	case "Page.captureScreenshot":
		clip := c.Params["clip"].(map[string]any)
		w := int(clip["width"].(float64) * clip["scale"].(float64))
		h := int(clip["height"].(float64) * clip["scale"].(float64))
		return map[string]any{"data": r.png(w, h)}, ""
	}
	return defaultHook(f, c)
}

func newShotSession(t *testing.T, r *shotRig) (*Session, *fakeCDP) {
	t.Helper()
	l := &fakeLauncher{t: t, hook: r.hook}
	s, err := Start(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: 5e9})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	return s, f
}

func decodeJPEG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func near(c color.Color, r, g, b, tol int) bool {
	cr, cg, cb, _ := c.RGBA()
	d := func(a uint32, want int) bool { x := int(a>>8) - want; return x >= -tol && x <= tol }
	return d(cr, r) && d(cg, g) && d(cb, b)
}

func TestScreenshotRedactsPasswordBoxes(t *testing.T) {
	rig := &shotRig{vw: 400, vh: 200, boxes: [][]float64{{50, 60, 150, 60, 150, 90, 50, 90}}}
	rig.png = func(w, h int) string { return solidPNG(t, w, h, color.RGBA{0, 0, 255, 255}) }
	s, _ := newShotSession(t, rig)
	shot, err := s.Screenshot(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if shot.Mime != "image/jpeg" || shot.Width != 400 || shot.Height != 200 {
		t.Fatalf("shot: %s %dx%d", shot.Mime, shot.Width, shot.Height)
	}
	img := decodeJPEG(t, shot.Data)
	if !near(img.At(100, 75), 128, 128, 128, 25) {
		t.Fatalf("the password box must be painted over, pixel is %v", img.At(100, 75))
	}
	if !near(img.At(5, 5), 0, 0, 255, 40) || !near(img.At(390, 190), 0, 0, 255, 40) {
		t.Fatal("pixels outside the box must be untouched")
	}
	if near(img.At(100, 75), 0, 0, 255, 60) {
		t.Fatal("the password region still shows the page")
	}
}

func TestScreenshotRedactionFollowsTheScale(t *testing.T) {
	// 3136x1000 css px: scale 0.5, so the box (200..400 x 100..200) is at 100..200 x 50..100 px.
	rig := &shotRig{vw: 3136, vh: 1000, boxes: [][]float64{{200, 100, 400, 100, 400, 200, 200, 200}}}
	rig.png = func(w, h int) string { return solidPNG(t, w, h, color.RGBA{0, 0, 255, 255}) }
	s, f := newShotSession(t, rig)
	shot, err := s.Screenshot(context.Background(), "T1")
	if err != nil {
		t.Fatal(err)
	}
	if shot.Width != 1568 || shot.Height != 500 {
		t.Fatalf("long side must be 1568, got %dx%d", shot.Width, shot.Height)
	}
	clip := f.callsFor("Page.captureScreenshot")[0].Params["clip"].(map[string]any)
	if clip["scale"] != 0.5 {
		t.Fatalf("scale %v", clip["scale"])
	}
	img := decodeJPEG(t, shot.Data)
	if !near(img.At(150, 75), 128, 128, 128, 25) {
		t.Fatalf("the redaction must follow the scale: %v", img.At(150, 75))
	}
	if !near(img.At(400, 75), 0, 0, 255, 40) {
		t.Fatalf("outside the scaled box stays untouched: %v", img.At(400, 75))
	}
}

func TestScreenshotIsClippedToTheViewport(t *testing.T) {
	rig := &shotRig{vw: 800, vh: 600, px: 30, py: 1200}
	rig.png = func(w, h int) string { return solidPNG(t, w, h, color.RGBA{9, 9, 9, 255}) }
	s, f := newShotSession(t, rig)
	if _, err := s.Screenshot(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	p := f.callsFor("Page.captureScreenshot")[0].Params
	clip := p["clip"].(map[string]any)
	if clip["x"] != 30.0 || clip["y"] != 1200.0 || clip["width"] != 800.0 || clip["height"] != 600.0 {
		t.Fatalf("the capture must be the visible viewport: %v", clip)
	}
	if p["captureBeyondViewport"] != false || p["format"] != "png" {
		t.Fatalf("params: %v", p)
	}
}

func TestScreenshotFailsClosedWhenPasswordFieldsCannotBeLocated(t *testing.T) {
	rig := &shotRig{vw: 100, vh: 100, queryErr: "boom"}
	rig.png = func(w, h int) string { return solidPNG(t, w, h, color.RGBA{1, 1, 1, 255}) }
	s, f := newShotSession(t, rig)
	if _, err := s.Screenshot(context.Background(), ""); err == nil {
		t.Fatal("no password lookup means no screenshot")
	}
	if len(f.callsFor("Page.captureScreenshot")) != 0 {
		t.Fatal("the page must not even be captured")
	}
	// A box-model failure of an input is also fatal unless it only means "no box".
	rig2 := &shotRig{vw: 100, vh: 100, boxes: [][]float64{{1, 1, 2, 1, 2, 2, 1, 2}}, boxErr: "node is detached"}
	rig2.png = rig.png
	s2, _ := newShotSession(t, rig2)
	if _, err := s2.Screenshot(context.Background(), ""); err == nil {
		t.Fatal("an unexplained box failure must refuse the screenshot")
	}
	rig3 := &shotRig{vw: 100, vh: 100, boxes: [][]float64{{1, 1, 2, 1, 2, 2, 1, 2}}, boxErr: "Could not compute box model."}
	rig3.png = rig.png
	s3, _ := newShotSession(t, rig3)
	if _, err := s3.Screenshot(context.Background(), ""); err != nil {
		t.Fatalf("a hidden (display:none) password input shows nothing and must not block: %v", err)
	}
}

func TestScreenshotSizeCapStepsQualityThenRefuses(t *testing.T) {
	rig := &shotRig{vw: 300, vh: 300}
	rig.png = func(w, h int) string { return noisePNG(t, w, h) }
	s, _ := newShotSession(t, rig)
	old := maxShotBytes
	defer func() { maxShotBytes = old }()

	// Sizes at the first and last quality, to place the cap between them.
	raw, _ := base64.StdEncoding.DecodeString(noisePNG(t, 300, 300))
	src, _ := png.Decode(bytes.NewReader(raw))
	size := func(q int) int {
		var b bytes.Buffer
		_ = jpeg.Encode(&b, src, &jpeg.Options{Quality: q})
		return b.Len()
	}
	hi, lo := size(80), size(40)
	if lo >= hi {
		t.Fatalf("test setup: quality 40 (%d) must be smaller than 80 (%d)", lo, hi)
	}
	maxShotBytes = (hi + lo) / 2
	shot, err := s.Screenshot(context.Background(), "")
	if err != nil || len(shot.Data) > maxShotBytes {
		t.Fatalf("a lower quality must be tried until it fits: %v len=%d cap=%d", err, len(shot.Data), maxShotBytes)
	}
	maxShotBytes = lo - 1
	if _, err := s.Screenshot(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("over the cap at the lowest quality must be refused: %v", err)
	}
}

func TestScreenshotRejectsAnUnusableCapture(t *testing.T) {
	rig := &shotRig{vw: 100, vh: 100}
	rig.png = func(w, h int) string { return "not base64 !!" }
	s, _ := newShotSession(t, rig)
	if _, err := s.Screenshot(context.Background(), ""); err == nil {
		t.Fatal("garbage must be an error")
	}
	rig2 := &shotRig{vw: 0, vh: 0}
	rig2.png = func(w, h int) string { return "" }
	s2, _ := newShotSession(t, rig2)
	if _, err := s2.Screenshot(context.Background(), ""); err == nil {
		t.Fatal("no viewport must be an error")
	}
}
