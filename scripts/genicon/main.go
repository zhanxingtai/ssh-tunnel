//go:build ignore

// Command genicon draws the icon of ssh-tunnel and writes it in the formats
// the project needs: an SVG master for the repository, PNG images for the
// documentation and an ICO file for the Windows tray.
//
// The drawing is defined once, in geometry constants, so that the vector and
// the raster versions stay in sync.
//
// Usage:
//
//	go run scripts/genicon/main.go
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

// The icon is drawn on a 512x512 canvas.
const canvasSize = 512

// The colours of the icon.
var (
	colTop    = color.NRGBA{R: 0x3f, G: 0x83, B: 0xf8, A: 0xff}
	colBottom = color.NRGBA{R: 0x1d, G: 0x3f, B: 0xa8, A: 0xff}
	colWhite  = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colGreen  = color.NRGBA{R: 0x4a, G: 0xde, B: 0x80, A: 0xff}
	colLine   = color.NRGBA{R: 0x4a, G: 0xde, B: 0x80, A: 0xb8}
	// colHole is the colour of the background in the middle of the icon; it
	// is used for the keyhole of the lock.
	colHole = color.NRGBA{R: 0x2f, G: 0x62, B: 0xd2, A: 0xff}
)

// The geometry of the icon.
//
// A reversed S runs from an endpoint in the top left corner to a person in
// the bottom right corner. A padlock sits in the middle of the curve: the
// tunnel is protected by the SSH connection. The line is thinner than the
// endpoints so that the contour stays visible at small sizes.
var (
	bgRadius = 112.0 // corner radius of the background

	// The reversed S is made of two open semicircles: the upper one is the
	// right half of a circle (missing its left side), the lower one is the
	// left half (missing its right side). Horizontal segments join them to
	// the endpoints and to the lock.
	sStartX, sStartY  = 136.0, 144.0
	sMidY             = 256.0
	sEndX, sEndY      = 376.0, 384.0
	sUpperCX, sUpperR = 340.0, 56.0
	sLowerCX, sLowerR = 172.0, 64.0
	sRadius           = 14.0 // half of the width of the line
	nodeR             = 38.0 // radius of the top left endpoint

	// The person at the bottom right endpoint: a head and shoulders.
	headCX, headCY, headR = 376.0, 332.0, 28.0
	bodyX0, bodyY0        = 326.0, 360.0
	bodyX1, bodyY1        = 426.0, 414.0
	bodyTopRadius         = 30.0
	bodyBottomRadius      = 16.0

	lockX0, lockY0 = 196.0, 228.0 // the body of the lock
	lockX1, lockY1 = 316.0, 324.0
	lockRadius     = 26.0 // only the bottom corners are rounded

	shackleCX, shackleCY = 256.0, 228.0
	shackleOuter         = 48.0
	shackleInner         = 22.0

	keyholeCX, keyholeCY, keyholeR = 256.0, 266.0, 14.0
	keyholeX0, keyholeY0           = 249.0, 266.0
	keyholeX1, keyholeY1           = 263.0, 304.0
	keyholeRadius                  = 7.0
)

func main() {
	root := repoRoot()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		fail(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "assets"), 0o755); err != nil {
		fail(err)
	}

	// The vector master used by the repository page and by the interface.
	svg := []byte(svgSource())
	write(filepath.Join(root, "assets", "icon.svg"), svg)
	if err := os.MkdirAll(filepath.Join(root, "internal", "gui", "assets"), 0o755); err != nil {
		fail(err)
	}
	write(filepath.Join(root, "internal", "gui", "assets", "icon.svg"), svg)

	// The raster versions.
	sizes := []int{16, 32, 48, 64, 128, 256, 512}
	images := make([][]byte, 0, len(sizes))
	for _, size := range sizes {
		data, err := renderPNG(size)
		if err != nil {
			fail(err)
		}
		images = append(images, data)
		switch size {
		case 512:
			write(filepath.Join(root, "assets", "icon-512.png"), data)
		case 256:
			write(filepath.Join(root, "assets", "icon-256.png"), data)
		case 64:
			write(filepath.Join(root, "assets", "icon-64.png"), data)
		case 32:
			write(filepath.Join(root, "assets", "icon-32.png"), data)
		}
	}
	// The icon file only holds the sizes an icon file can describe; the
	// 512 pixel image stays in the assets directory.
	write(filepath.Join(root, "internal", "assets", "icon.ico"), ico(images[:6], sizes[:6]))
	fmt.Println("icon written")
}

func write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fail(err)
	}
	fmt.Println("wrote", path)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// repoRoot returns the root of the repository, derived from the path of the
// source file.
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fail(errors.New("cannot locate the source file"))
	}
	// The file lives in scripts/genicon.
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

/* ------------------------------------------------------------------ */
/* raster drawing                                                     */
/* ------------------------------------------------------------------ */

// supersample is the factor used to smooth the edges of the shapes.
const supersample = 4

// canvas is a simple RGBA drawing surface.
type canvas struct {
	w, h int
	px   []color.NRGBA
}

func newCanvas(w, h int) *canvas {
	return &canvas{w: w, h: h, px: make([]color.NRGBA, w*h)}
}

// blend composites a colour over the pixel at x, y.
func (c *canvas) blend(x, y int, col color.NRGBA) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h || col.A == 0 {
		return
	}
	i := y*c.w + x
	dst := c.px[i]
	a := float64(col.A) / 255
	da := float64(dst.A) / 255
	outA := a + da*(1-a)
	if outA <= 0 {
		c.px[i] = color.NRGBA{}
		return
	}
	mix := func(s, d uint8) uint8 {
		v := (float64(s)*a + float64(d)*da*(1-a)) / outA
		return clamp8(v)
	}
	c.px[i] = color.NRGBA{
		R: mix(col.R, dst.R),
		G: mix(col.G, dst.G),
		B: mix(col.B, dst.B),
		A: clamp8(outA * 255),
	}
}

// fillRoundedRect paints a rectangle with rounded corners. The radii are
// given per corner in the order top-left, top-right, bottom-right and
// bottom-left; a zero radius gives a square corner.
func (c *canvas) fillRoundedRect(x0, y0, x1, y1, rtl, rtr, rbr, rbl float64, col color.NRGBA) {
	for y := 0; y < c.h; y++ {
		fy := float64(y) + 0.5
		for x := 0; x < c.w; x++ {
			fx := float64(x) + 0.5
			if !insideRoundedRect(fx, fy, x0, y0, x1, y1, rtl, rtr, rbr, rbl) {
				continue
			}
			c.blend(x, y, col)
		}
	}
}

// insideRoundedRect reports whether a point is inside a rectangle with
// rounded corners.
func insideRoundedRect(x, y, x0, y0, x1, y1, rtl, rtr, rbr, rbl float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	var cx, cy, r float64
	switch {
	case rtl > 0 && x < x0+rtl && y < y0+rtl:
		cx, cy, r = x0+rtl, y0+rtl, rtl
	case rtr > 0 && x > x1-rtr && y < y0+rtr:
		cx, cy, r = x1-rtr, y0+rtr, rtr
	case rbr > 0 && x > x1-rbr && y > y1-rbr:
		cx, cy, r = x1-rbr, y1-rbr, rbr
	case rbl > 0 && x < x0+rbl && y > y1-rbl:
		cx, cy, r = x0+rbl, y1-rbl, rbl
	default:
		return true
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// point is a position on the canvas.
type point struct {
	x, y float64
}

// fillPolyline paints a polyline with round caps and joins. The pixels of
// overlapping segments are blended once, so the joints keep the alpha of the
// rest of the line.
func (c *canvas) fillPolyline(points []point, r float64, col color.NRGBA) {
	if len(points) == 0 {
		return
	}
	mask := make([]bool, c.w*c.h)
	r2 := r * r
	pad := int(r) + 2
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		minX, maxX := int(a.x), int(b.x)
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		minY, maxY := int(a.y), int(b.y)
		if minY > maxY {
			minY, maxY = maxY, minY
		}
		minX -= pad
		maxX += pad
		minY -= pad
		maxY += pad
		if minX < 0 {
			minX = 0
		}
		if minY < 0 {
			minY = 0
		}
		if maxX > c.w {
			maxX = c.w
		}
		if maxY > c.h {
			maxY = c.h
		}
		for y := minY; y < maxY; y++ {
			fy := float64(y) + 0.5
			for x := minX; x < maxX; x++ {
				fx := float64(x) + 0.5
				if distanceSquaredToSegment(fx, fy, a, b) <= r2 {
					mask[y*c.w+x] = true
				}
			}
		}
	}
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			if mask[y*c.w+x] {
				c.blend(x, y, col)
			}
		}
	}
}

// arcPoints samples a circular arc. The points are used to render a smooth
// stroke with fillPolyline.
func arcPoints(cx, cy, r, start, end float64, steps int) []point {
	out := make([]point, steps+1)
	for i := 0; i <= steps; i++ {
		a := start + (end-start)*float64(i)/float64(steps)
		out[i] = point{
			x: cx + r*math.Cos(a),
			y: cy + r*math.Sin(a),
		}
	}
	return out
}

// distanceSquaredToSegment returns the squared distance from a point to a
// line segment.
func distanceSquaredToSegment(x, y float64, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	length := dx*dx + dy*dy
	t := 0.0
	if length > 0 {
		t = ((x-a.x)*dx + (y-a.y)*dy) / length
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
	}
	px, py := a.x+t*dx, a.y+t*dy
	ex, ey := x-px, y-py
	return ex*ex + ey*ey
}

// fillCircle paints a disc.
func (c *canvas) fillCircle(cx, cy, r float64, col color.NRGBA) {
	for y := 0; y < c.h; y++ {
		fy := float64(y) + 0.5
		for x := 0; x < c.w; x++ {
			fx := float64(x) + 0.5
			dx, dy := fx-cx, fy-cy
			if dx*dx+dy*dy <= r*r {
				c.blend(x, y, col)
			}
		}
	}
}

// fillUpperBand paints the upper half of a ring: the shackle of the lock.
func (c *canvas) fillUpperBand(cx, cy, outer, inner float64, col color.NRGBA) {
	for y := 0; y < c.h; y++ {
		fy := float64(y) + 0.5
		if fy > cy {
			continue
		}
		for x := 0; x < c.w; x++ {
			fx := float64(x) + 0.5
			dx, dy := fx-cx, fy-cy
			d := dx*dx + dy*dy
			if d <= outer*outer && d >= inner*inner {
				c.blend(x, y, col)
			}
		}
	}
}

// fillGradient paints a rectangle with a diagonal gradient and rounded
// corners.
func (c *canvas) fillGradient(x0, y0, x1, y1, radius float64, top, bottom color.NRGBA) {
	for y := 0; y < c.h; y++ {
		fy := float64(y) + 0.5
		for x := 0; x < c.w; x++ {
			fx := float64(x) + 0.5
			if !insideRoundedRect(fx, fy, x0, y0, x1, y1, radius, radius, radius, radius) {
				continue
			}
			t := (fx + fy) / (x1 + y1)
			col := color.NRGBA{
				R: mixChannel(top.R, bottom.R, t),
				G: mixChannel(top.G, bottom.G, t),
				B: mixChannel(top.B, bottom.B, t),
				A: 0xff,
			}
			c.blend(x, y, col)
		}
	}
}

func mixChannel(a, b uint8, t float64) uint8 {
	return clamp8(float64(a) + (float64(b)-float64(a))*t)
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// image scales the canvas down and returns the result.
func (c *canvas) image(size int) *image.RGBA {
	factor := c.w / size
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < factor; sy++ {
				for sx := 0; sx < factor; sx++ {
					p := c.px[(y*factor+sy)*c.w+(x*factor+sx)]
					pa := float64(p.A) / 255
					r += float64(p.R) * pa
					g += float64(p.G) * pa
					b += float64(p.B) * pa
					a += pa
				}
			}
			n := float64(factor * factor)
			if a > 0 {
				out.Set(x, y, color.RGBA{
					R: clamp8(r / a),
					G: clamp8(g / a),
					B: clamp8(b / a),
					A: clamp8(a / n * 255),
				})
			}
		}
	}
	return out
}

// draw renders the icon on a canvas of the given size.
func draw(size int) *canvas {
	scale := float64(size) / canvasSize
	c := newCanvas(size, size)
	s := func(v float64) float64 { return v * scale }

	c.fillGradient(0, 0, s(canvasSize), s(canvasSize), s(bgRadius), colTop, colBottom)
	curve := []point{
		{s(sStartX), s(sStartY)},
		{s(sUpperCX), s(sStartY)},
	}
	curve = append(curve, arcPoints(
		s(sUpperCX), s(sStartY+sUpperR), s(sUpperR),
		-math.Pi/2, math.Pi/2, 64,
	)[1:]...)
	curve = append(curve, point{s(sLowerCX), s(sMidY)})
	curve = append(curve, arcPoints(
		s(sLowerCX), s(sMidY+sLowerR), s(sLowerR),
		-math.Pi/2, -3*math.Pi/2, 64,
	)[1:]...)
	curve = append(curve, point{s(sEndX), s(sEndY)})
	c.fillPolyline(curve, s(sRadius), colLine)
	c.fillCircle(s(sStartX), s(sStartY), s(nodeR), colGreen)
	c.fillCircle(s(headCX), s(headCY), s(headR), colGreen)
	c.fillRoundedRect(s(bodyX0), s(bodyY0), s(bodyX1), s(bodyY1),
		s(bodyTopRadius), s(bodyTopRadius), s(bodyBottomRadius), s(bodyBottomRadius), colGreen)
	c.fillUpperBand(s(shackleCX), s(shackleCY), s(shackleOuter), s(shackleInner), colWhite)
	c.fillRoundedRect(s(lockX0), s(lockY0), s(lockX1), s(lockY1),
		0, 0, s(lockRadius), s(lockRadius), colWhite)
	c.fillCircle(s(keyholeCX), s(keyholeCY), s(keyholeR), colHole)
	c.fillRoundedRect(s(keyholeX0), s(keyholeY0), s(keyholeX1), s(keyholeY1),
		s(keyholeRadius), s(keyholeRadius), s(keyholeRadius), s(keyholeRadius), colHole)
	return c
}

// renderPNG renders the icon and encodes it as a PNG file.
func renderPNG(size int) ([]byte, error) {
	img := draw(size * supersample).image(size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ico builds an icon file holding the PNG images.
func ico(images [][]byte, sizes []int) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&buf, binary.LittleEndian, uint16(len(images)))
	offset := 6 + 16*len(images)
	for i, data := range images {
		dim := byte(sizes[i])
		if sizes[i] >= 256 {
			dim = 0
		}
		buf.WriteByte(dim)                                  // width
		buf.WriteByte(dim)                                  // height
		buf.WriteByte(0)                                    // colours
		buf.WriteByte(0)                                    // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // colour planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(data)
	}
	for _, data := range images {
		buf.Write(data)
	}
	return buf.Bytes()
}

/* ------------------------------------------------------------------ */
/* vector drawing                                                     */
/* ------------------------------------------------------------------ */

// svgSource returns the SVG master of the icon. The numbers come from the
// same constants as the raster drawing.
func svgSource() string {
	sCurve := fmt.Sprintf(
		"M %g %g H %g A %g %g 0 0 1 %g %g H %g A %g %g 0 0 0 %g %g H %g",
		sStartX, sStartY, sUpperCX,
		sUpperR, sUpperR, sUpperCX, sMidY,
		sLowerCX,
		sLowerR, sLowerR, sLowerCX, sEndY,
		sEndX)
	body := fmt.Sprintf(
		"M %g %g V %g a %g %g 0 0 1 %g %g H %g a %g %g 0 0 1 %g %g V %g a %g %g 0 0 1 %g %g H %g a %g %g 0 0 1 %g %g Z",
		bodyX0, bodyY1, bodyY0+bodyTopRadius,
		bodyTopRadius, bodyTopRadius, bodyTopRadius, -bodyTopRadius,
		bodyX1-bodyTopRadius,
		bodyTopRadius, bodyTopRadius, bodyTopRadius, bodyTopRadius,
		bodyY1-bodyBottomRadius,
		bodyBottomRadius, bodyBottomRadius, -bodyBottomRadius, bodyBottomRadius,
		bodyX0+bodyBottomRadius,
		bodyBottomRadius, bodyBottomRadius, -bodyBottomRadius, -bodyBottomRadius)
	shackle := fmt.Sprintf(
		"M %g %g a %g %g 0 0 1 %g 0",
		shackleCX-shackleOuter, shackleCY, shackleOuter, shackleOuter, shackleOuter*2)
	lock := fmt.Sprintf(
		"M %g %g H %g V %g a %g %g 0 0 1 %g %g H %g a %g %g 0 0 1 %g %g Z",
		lockX0, lockY0, lockX1, lockY1-lockRadius, lockRadius, lockRadius,
		-lockRadius, lockRadius, lockX0+lockRadius, lockRadius, lockRadius,
		-lockRadius, -lockRadius)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="ssh-tunnel">
  <title>ssh-tunnel</title>
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#%02x%02x%02x"/>
      <stop offset="1" stop-color="#%02x%02x%02x"/>
    </linearGradient>
  </defs>
  <rect x="0" y="0" width="%d" height="%d" rx="%g" fill="url(#bg)"/>
  <path d="%s" fill="none" stroke="#%02x%02x%02x" stroke-opacity="%g" stroke-width="%g" stroke-linecap="round" stroke-linejoin="round"/>
  <circle cx="%g" cy="%g" r="%g" fill="#%02x%02x%02x"/>
  <circle cx="%g" cy="%g" r="%g" fill="#%02x%02x%02x"/>
  <path d="%s" fill="#%02x%02x%02x"/>
  <path d="%s" fill="none" stroke="#ffffff" stroke-width="%g"/>
  <path d="%s" fill="#ffffff"/>
  <circle cx="%g" cy="%g" r="%g" fill="#%02x%02x%02x"/>
  <rect x="%g" y="%g" width="%g" height="%g" rx="%g" fill="#%02x%02x%02x"/>
</svg>
`,
		canvasSize, canvasSize, canvasSize, canvasSize,
		colTop.R, colTop.G, colTop.B, colBottom.R, colBottom.G, colBottom.B,
		canvasSize, canvasSize, bgRadius,
		sCurve, colLine.R, colLine.G, colLine.B, float64(colLine.A)/255, sRadius*2,
		sStartX, sStartY, nodeR, colGreen.R, colGreen.G, colGreen.B,
		headCX, headCY, headR, colGreen.R, colGreen.G, colGreen.B,
		body, colGreen.R, colGreen.G, colGreen.B,
		shackle, shackleOuter-shackleInner,
		lock,
		keyholeCX, keyholeCY, keyholeR, colHole.R, colHole.G, colHole.B,
		keyholeX0, keyholeY0, keyholeX1-keyholeX0, keyholeY1-keyholeY0, keyholeRadius,
		colHole.R, colHole.G, colHole.B,
	)
}
