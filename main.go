package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Game constants (logical pixel space 400×600) ─────────────────────────────

const (
	gameW = 400.0
	gameH = 600.0

	birdX      = 80.0
	birdRadius = 20.0

	gravity   = 0.28
	flapForce = -6.5

	pipeW      = 65.0
	pipeGap    = 160.0
	pipeSpeed  = 2.8
	pipePeriod = 90 // frames between new pipe pairs

	groundH = 70.0
)

// ── State ─────────────────────────────────────────────────────────────────────

type State int

const (
	StateStart State = iota
	StatePlaying
	StateOver
)

// ── Pipe ──────────────────────────────────────────────────────────────────────

type Pipe struct {
	x, gapY float64
}

// ── Game ──────────────────────────────────────────────────────────────────────

type Game struct {
	mu        sync.Mutex
	state     State
	birdY     float64
	birdVY    float64
	pipes     []Pipe
	score     int
	hiScore   int
	frame     int
	flapTimer int
	deathTime time.Time
	prefs     fyne.Preferences

	raster    *canvas.Raster
	scoreText *canvas.Text
	msgText   *canvas.Text
	subText   *canvas.Text

	cloudCircles [numClouds * puffsPerCloud]*canvas.Circle
	groundBar    *canvas.Rectangle
	overlay      *fyne.Container
}

func NewGame(prefs fyne.Preferences) *Game {
	g := &Game{
		birdY:   gameH / 2,
		prefs:   prefs,
		hiScore: prefs.IntWithFallback("hiScore", 0),
	}

	g.raster = canvas.NewRaster(g.drawFrame)
	g.raster.SetMinSize(fyne.NewSize(gameW, gameH))

	g.scoreText = canvas.NewText("0", color.White)
	g.scoreText.TextSize = 44
	g.scoreText.TextStyle = fyne.TextStyle{Bold: true}
	g.scoreText.Alignment = fyne.TextAlignCenter

	g.msgText = canvas.NewText("FLAPPY GOPHER", color.White)
	g.msgText.TextSize = 34
	g.msgText.TextStyle = fyne.TextStyle{Bold: true}
	g.msgText.Alignment = fyne.TextAlignCenter

	txt := "hit space/enter to play"
	if fyne.CurrentDevice().IsMobile() {
		txt = "tap to play"
	}
	g.subText = canvas.NewText(txt, color.RGBA{R: 255, G: 240, B: 80, A: 255})
	g.subText.TextSize = 18
	g.subText.Alignment = fyne.TextAlignCenter

	// Build the overlay layer: cloud circles + ground bar.
	cloudCol := color.NRGBA{R: 245, G: 250, B: 255, A: 220}
	var objs []fyne.CanvasObject
	for i := range numClouds {
		for j := range puffsPerCloud {
			c := canvas.NewCircle(cloudCol)
			g.cloudCircles[i*puffsPerCloud+j] = c
			objs = append(objs, c)
		}
	}
	g.groundBar = canvas.NewRectangle(color.RGBA{R: 145, G: 133, B: 118, A: 255})
	objs = append(objs, g.groundBar)
	g.overlay = container.NewWithoutLayout(objs...)

	return g
}

// flap is called on Space key or tap – transitions game state and applies upward impulse.
func (g *Game) flap() {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch g.state {
	case StateStart:
		g.state = StatePlaying
		g.birdVY = flapForce
		g.flapTimer = 20
		go playJingle()
	case StatePlaying:
		g.birdVY = flapForce
		g.flapTimer = 20
		go playFlap()
	case StateOver:
		if time.Since(g.deathTime) < 100*time.Millisecond {
			return
		}
		if g.score > g.hiScore {
			g.hiScore = g.score
			g.prefs.SetInt("hiScore", g.hiScore)
		}
		g.birdY = gameH / 2
		g.birdVY = flapForce
		g.pipes = nil
		g.score = 0
		g.frame = 0
		g.flapTimer = 20
		g.state = StatePlaying
		go playJingle()
	}
}

// update advances physics and checks collisions; called every tick.
func (g *Game) update() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != StatePlaying {
		return
	}

	g.frame++
	if g.flapTimer > 0 {
		g.flapTimer--
	}
	g.birdVY += gravity
	g.birdY += g.birdVY

	// Spawn a new pipe pair.
	if g.frame%pipePeriod == 1 {
		lo := pipeGap/2 + 50
		hi := gameH - groundH - pipeGap/2 - 50
		gapY := lo + rand.Float64()*(hi-lo)
		g.pipes = append(g.pipes, Pipe{x: gameW + 10, gapY: gapY})
	}

	// Move pipes left; score when bird clears the right edge.
	var keep []Pipe
	for _, p := range g.pipes {
		p.x -= pipeSpeed
		oldRight := p.x + pipeW + pipeSpeed
		if oldRight >= birdX && p.x+pipeW < birdX {
			g.score++
		}
		if p.x+pipeW > 0 {
			keep = append(keep, p)
		}
	}
	g.pipes = keep

	// Ground / ceiling collision.
	if g.birdY-birdRadius <= 0 || g.birdY+birdRadius >= gameH-groundH {
		g.state = StateOver
		g.deathTime = time.Now()
		go playSplat()
		return
	}
	// Pipe collision.
	for _, p := range g.pipes {
		if pipeCollides(g.birdY, p) {
			g.state = StateOver
			g.deathTime = time.Now()
			go playSplat()
			return
		}
	}
}

func pipeCollides(birdY float64, p Pipe) bool {
	if birdX+birdRadius < p.x || birdX-birdRadius > p.x+pipeW {
		return false
	}
	topEdge := p.gapY - pipeGap/2
	botEdge := p.gapY + pipeGap/2
	return birdY-birdRadius < topEdge || birdY+birdRadius > botEdge
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func (g *Game) drawFrame(w, h int) image.Image {
	g.mu.Lock()
	state := g.state
	by := g.birdY
	bvy := g.birdVY
	ft := g.flapTimer
	pipes := make([]Pipe, len(g.pipes))
	copy(pipes, g.pipes)
	g.mu.Unlock()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	sx, sy := float64(w)/gameW, float64(h)/gameH
	gndY := int((gameH - groundH) * sy)

	// Sky gradient: light-blue top → clear blue → deeper horizon blue.
	for y := 0; y < gndY; y++ {
		t := float64(y) / float64(gndY)
		var r, gg, b uint8
		if t < 0.15 {
			u := t / 0.15 // 0→1 over the top 15%
			r, gg, b = clamp8(lerp(185, 135, u)), clamp8(lerp(225, 206, u)), clamp8(lerp(250, 235, u))
		} else {
			u := (t - 0.15) / 0.85 // 0→1 over the lower 85%
			r, gg, b = clamp8(lerp(135, 80, u)), clamp8(lerp(206, 160, u)), clamp8(lerp(235, 210, u))
		}
		off := y * img.Stride
		for x := 0; x < w; x++ {
			img.Pix[off+x*4], img.Pix[off+x*4+1], img.Pix[off+x*4+2], img.Pix[off+x*4+3] = r, gg, b, 255
		}
	}

	// Ground (rocky texture).
	drawGround(img, gndY, w, h)

	// Pipes.
	for _, p := range pipes {
		drawPipe(img, p, sx, sy, gndY, w, h)
	}

	// Bird / logo.
	if state == StateStart {
		// Draw a larger, centred gopher as the title screen logo.
		lx := w / 2
		ly := int(float64(h) * 0.30)
		lr := max(int(38*math.Min(sx, sy)), 12)
		drawBird(img, lx, ly, lr, 0, 0, w, h)
	} else {
		bx := int(birdX * sx)
		byPx := int(by * sy)
		br := max(int(birdRadius*math.Min(sx, sy)), 8)
		drawBird(img, bx, byPx, br, bvy, ft, w, h)
	}

	return img
}

func lerp(a, b uint8, t float64) float64 { return float64(a)*(1-t) + float64(b)*t }
func clamp8(v float64) uint8              { return uint8(v) }

// groundHash returns a deterministic value in [-32, 31] for a given (x, y).
func groundHash(x, y int) int {
	h := uint32(x)*2654435761 ^ uint32(y)*2246822519
	h ^= h >> 15
	h *= 2246822519
	h ^= h >> 13
	return int(h&0x3F) - 32
}

// drawGround renders a rocky ledge with horizontal strata, per-pixel grain,
// a dark shadow lip at the top, and sparse crack pixels.
func drawGround(img *image.NRGBA, gndY, w, h int) {
	for y := gndY; y < h; y++ {
		off := y * img.Stride
		dy := y - gndY
		// Strata: a single band offset shared across all x in a 10-px-tall row.
		band := groundHash(0, (y/10)*10) / 2 // [-16, 15]
		for x := 0; x < w; x++ {
			var rv, gv, bv uint8
			if dy < 4 {
				// Dark overhang shadow.
				shade := uint8(50 + dy*8)
				rv, gv, bv = shade, shade-5, shade-10
			} else {
				grain := groundHash(x, y) >> 2 // [-8, 7] per-pixel noise
				v := band + grain              // [-24, 22]
				rv = uint8(145 + v)
				gv = uint8(133 + v)
				bv = uint8(118 + v)
				// Sparse dark crack pixels (~1 in 128).
				if groundHash(x+37, y+13)&0x7F == 0 {
					rv, gv, bv = 72, 64, 56
				}
			}
			img.Pix[off+x*4] = rv
			img.Pix[off+x*4+1] = gv
			img.Pix[off+x*4+2] = bv
			img.Pix[off+x*4+3] = 255
		}
	}
}

func drawPipe(img *image.NRGBA, p Pipe, sx, sy float64, gndY, W, H int) {
	x1 := int(p.x * sx)
	x2 := int((p.x + pipeW) * sx)
	topB := int((p.gapY - pipeGap/2) * sy)
	botT := int((p.gapY + pipeGap/2) * sy)
	capH := max(int(14*sy), 7)
	cx1 := x1 - max(int(5*sx), 3)
	cx2 := x2 + max(int(5*sx), 3)

	fill := color.NRGBA{R: 80, G: 195, B: 78, A: 255}
	edge := color.NRGBA{R: 50, G: 140, B: 45, A: 255}
	cap1 := color.NRGBA{R: 65, G: 178, B: 63, A: 255}
	cape := color.NRGBA{R: 40, G: 125, B: 38, A: 255}

	drawRect(img, x1, 0, x2, topB-capH, fill, edge, W, H)
	drawRect(img, cx1, topB-capH, cx2, topB, cap1, cape, W, H)
	drawRect(img, x1, botT+capH, x2, gndY, fill, edge, W, H)
	drawRect(img, cx1, botT, cx2, botT+capH, cap1, cape, W, H)

	// Shine – a narrow bright strip near the left of each section to fake a
	// cylindrical highlight. Two strips: a bright leading edge then a softer mid.
	sw := max(int(3*sx), 2)
	shx := x1 + max(int(3*sx), 2)  // just inside the left dark edge
	shcx := cx1 + max(int(3*sx), 2)
	hi1 := color.NRGBA{R: 165, G: 235, B: 135, A: 255}
	hi2 := color.NRGBA{R: 120, G: 215, B: 100, A: 255}

	// Shaft shine
	drawRect(img, shx, 0, shx+sw, topB-capH, hi1, hi1, W, H)
	drawRect(img, shx+sw, 0, shx+sw*3, topB-capH, hi2, hi2, W, H)
	drawRect(img, shx, botT+capH, shx+sw, gndY, hi1, hi1, W, H)
	drawRect(img, shx+sw, botT+capH, shx+sw*3, gndY, hi2, hi2, W, H)
	// Cap shine
	drawRect(img, shcx, topB-capH, shcx+sw, topB, hi1, hi1, W, H)
	drawRect(img, shcx+sw, topB-capH, shcx+sw*3, topB, hi2, hi2, W, H)
	drawRect(img, shcx, botT, shcx+sw, botT+capH, hi1, hi1, W, H)
	drawRect(img, shcx+sw, botT, shcx+sw*3, botT+capH, hi2, hi2, W, H)
}

func drawRect(img *image.NRGBA, x1, y1, x2, y2 int, fill, edge color.NRGBA, W, H int) {
	if x1 >= x2 || y1 >= y2 {
		return
	}
	x1, y1 = max(x1, 0), max(y1, 0)
	x2, y2 = min(x2, W), min(y2, H)
	for y := y1; y < y2; y++ {
		off := y * img.Stride
		for x := x1; x < x2; x++ {
			c := fill
			if x == x1 || x == x2-1 {
				c = edge
			}
			img.Pix[off+x*4], img.Pix[off+x*4+1], img.Pix[off+x*4+2], img.Pix[off+x*4+3] = c.R, c.G, c.B, c.A
		}
	}
}

func drawBird(img *image.NRGBA, cx, cy, r int, _ float64, flapTimer int, W, H int) {
	body  := color.NRGBA{R: 99,  G: 136, B: 168, A: 255} // classic Go gopher blue-grey
	snoutC := color.NRGBA{R: 185, G: 210, B: 220, A: 255} // lighter snout
	pawC  := color.NRGBA{R: 72,  G: 105, B: 132, A: 255} // slightly darker for paws
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	dark  := color.NRGBA{R: 15,  G: 15,  B: 15,  A: 255}

	flapping := flapTimer > 0

	// Front paw – rises when flapping.
	fpx := cx + r/3
	fpy := cy + r*3/4
	if flapping {
		fpy = cy - r*3/4
	}
	drawPaw(img, fpx, fpy, r/3, pawC, flapping, W, H)

	// Body.
	drawCircle(img, cx, cy, r, body, W, H)

	// Back paw – rises when flapping.
	bpx := cx - r*2/3
	bpy := cy + r*3/5
	if flapping {
		bpy = cy - r*3/5
	}
	drawPaw(img, bpx, bpy, r/3, pawC, flapping, W, H)


	// Snout (right-forward).
	snoutX := cx + r*3/5
	snoutY := cy + r/6
	snoutR := r * 2 / 5
	drawCircle(img, snoutX, snoutY, snoutR, snoutC, W, H)

	// Nose.
	drawCircle(img, snoutX+snoutR/3, snoutY-snoutR/3, max(r/7, 2), dark, W, H)

	// Teeth – two white rectangles below snout.
	tw := r / 5
	toothTop := snoutY + snoutR/2
	toothBot := toothTop + r*2/5
	drawRect(img, snoutX-tw-1, toothTop, snoutX-1, toothBot, white, white, W, H)
	drawRect(img, snoutX+1, toothTop, snoutX+tw+1, toothBot, white, white, W, H)

	// Eye (large white circle with pupil and shine).
	// Pupil shifts upward when flapping.
	eyeX := cx + r/4
	eyeY := cy - r/4
	pupilDY := 0
	if flapping {
		pupilDY = -3
	}
	drawCircle(img, eyeX, eyeY, r/3, white, W, H)
	drawCircle(img, eyeX+r/8, eyeY+pupilDY/2, r/5, dark, W, H)
	drawCircle(img, eyeX+r/8+3+pupilDY, eyeY+pupilDY, max(r/8, 1), white, W, H)
}

// drawPaw draws a small paw with three finger-nubs pointing up (flapping) or down (resting).
func drawPaw(img *image.NRGBA, cx, cy, r int, c color.NRGBA, flapping bool, W, H int) {
	drawCircle(img, cx, cy, r, c, W, H)
	fr := max(r/3, 2)
	dir := 1 // nubs point down when resting
	if flapping {
		dir = -1 // nubs point up when flapping
	}
	drawCircle(img, cx-r/2, cy+dir*(r-fr/2), fr, c, W, H)
	drawCircle(img, cx, cy+dir*r, fr, c, W, H)
	drawCircle(img, cx+r/2, cy+dir*(r-fr/2), fr, c, W, H)
}

func drawCircle(img *image.NRGBA, cx, cy, r int, c color.NRGBA, W, H int) {
	r2 := r * r
	for y := max(cy-r, 0); y <= min(cy+r, H-1); y++ {
		off := y * img.Stride
		for x := max(cx-r, 0); x <= min(cx+r, W-1); x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				img.Pix[off+x*4], img.Pix[off+x*4+1], img.Pix[off+x*4+2], img.Pix[off+x*4+3] = c.R, c.G, c.B, c.A
			}
		}
	}
}

// ── Tap widget ────────────────────────────────────────────────────────────────

// tapWidget is an invisible widget that fills its container and captures taps.
type tapWidget struct {
	widget.BaseWidget
	onTap func()
}

func newTapWidget(fn func()) *tapWidget {
	t := &tapWidget{onTap: fn}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (t *tapWidget) MouseDown(*desktop.MouseEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tapWidget) MouseUp(*desktop.MouseEvent) {}

func (t *tapWidget) TouchDown(*mobile.TouchEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tapWidget) TouchUp(*mobile.TouchEvent) {}
func (t *tapWidget) TouchCancel(*mobile.TouchEvent) {}

// ── Overlay (clouds + ground bar) ─────────────────────────────────────────────

const (
	numClouds    = 5
	puffsPerCloud = 5
)

// cloudPuffs gives (dxFrac, dyFrac, rFrac) relative to the cloud centre.
// Each puff circle's position = centre + offset*mainRadius.
var cloudPuffs = [puffsPerCloud][3]float32{
	{0, 0, 1},
	{-2.0 / 3, 1.0 / 3, 2.0 / 3},
	{2.0 / 3, 1.0 / 3, 2.0 / 3},
	{-1.0 / 3, -1.0 / 5, 4.0 / 5},
	{1.0 / 3, -1.0 / 5, 4.0 / 5},
}

type cloudSpec struct{ baseX, baseY, radius, speed float32 }

// cloudDefs lists each cloud.  All clouds are positioned near the top of the
// screen; negative baseY centres them above the safe-area edge so their lower
// puffs emerge into the status-bar inset area.
var cloudDefs = [numClouds]cloudSpec{
	{60, -22, 32, 10},
	{230, -10, 28, 8},
	{355, -5, 20, 13},
	{130, 18, 24, 7},
	{375, -15, 23, 12},
}

// updateOverlay repositions all cloud circles and the ground bar based on the
// current wall-clock time and the overlay's actual laid-out size.  It is safe
// to call from the game-loop goroutine; Fyne reads positions only during the
// subsequent canvas.Refresh.
func (g *Game) updateOverlay() {
	s := g.overlay.Size()
	if s.Width == 0 {
		return // not yet laid out
	}
	scaleX := s.Width / gameW
	scaleY := s.Height / gameH
	scale := min(scaleX, scaleY) // uniform radius scale keeps circles round

	// Use float64 for time: float32 only has ~7 significant digits and
	// UnixMilli() is ~1.7e12, so float32(UnixMilli)*0.001 loses sub-second
	// precision, making clouds appear stationary.
	tSec := float64(time.Now().UnixMilli()) * 0.001
	// cloudMargin is the off-screen buffer on each side.  A cloud wraps from
	// cx = −cloudMargin (fully off left) to cx = gameW+cloudMargin (fully off
	// right), so it always enters and exits while invisible.
	const cloudMargin = 150
	const span = float64(gameW + 2*cloudMargin)

	// On desktop the status-bar inset is absent, so nudge clouds down slightly
	// so they sit in a more natural position.
	var cloudYOffset float32
	if !fyne.CurrentDevice().IsMobile() {
		cloudYOffset = gameH * 0.08
	}

	for i, spec := range cloudDefs {
		elapsed := float32(math.Mod(tSec*float64(spec.speed), span))
		cx := spec.baseX - elapsed
		if cx < -cloudMargin {
			cx += float32(span)
		}
		for j, puff := range cloudPuffs {
			pr := spec.radius * puff[2] * scale
			px := cx + spec.radius*puff[0]
			py := spec.baseY + spec.radius*puff[1] + cloudYOffset
			idx := i*puffsPerCloud + j
			g.cloudCircles[idx].Move(fyne.NewPos(px*scaleX-pr, py*scaleY-pr))
			g.cloudCircles[idx].Resize(fyne.NewSize(pr*2, pr*2))
		}
	}

	// Ground bar: starts at gndY and extends well past the safe-area bottom so
	// it fills the navigation-bar / home-indicator inset space.
	gndPt := (gameH - 1) * scaleY
	g.groundBar.Move(fyne.NewPos(0, gndPt))
	g.groundBar.Resize(fyne.NewSize(s.Width, groundH))
}

// ── Theme ─────────────────────────────────────────────────────────────────────

// groundTheme wraps the default theme but sets the window background to the
// average ground colour. On mobile, the area below the safe zone (home
// indicator bar) inherits this colour, so the ground appears to extend all the
// way to the physical bottom of the screen.
type groundTheme struct{ fyne.Theme }

func (t groundTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameBackground {
		// Match the very top of the sky gradient so the status-bar inset blends
		// seamlessly with the sky. The ground at the bottom of the screen is
		// handled by the raster itself extending to the bottom of the safe area;
		// on devices with gesture/transparent navigation this is sufficient.
		return color.RGBA{R: 185, G: 225, B: 250, A: 255}
	}
	return t.Theme.Color(n, v)
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	initAudio()

	a := app.NewWithID("xyz.andy.flappy-gopher")
	th := groundTheme{theme.DefaultTheme()}
	a.Settings().SetTheme(th)
	w := a.NewWindow("Flappy Gopher")
	w.SetPadded(false)

	g := NewGame(a.Preferences())

	scoreBG := canvas.NewRectangle(th.Color(theme.ColorNameBackground, theme.VariantLight))
	scoreBG.CornerRadius = 4
	scoreBG.StrokeWidth = 2
	scoreBG.StrokeColor = color.RGBA{R: 145, G: 133, B: 118, A: 255}
	textBox := container.NewStack(scoreBG, container.NewPadded(g.scoreText))
	textBox.Hide()

	// Layer stack: raster → overlay (clouds + ground bar) → tap catcher → UI text.
	content := container.NewStack(
		g.raster,
		g.overlay,
		newTapWidget(g.flap),
		container.NewBorder(container.New(layout.NewCustomPaddedLayout(10, 0, 0, 0), container.NewCenter(textBox)), nil, nil, nil),
		container.NewCenter(container.NewVBox(g.msgText, g.subText)),
	)

	w.SetContent(content)
	w.Resize(fyne.NewSize(gameW, gameH))

	if desk, ok := w.Canvas().(desktop.Canvas); ok {
		// Keyboard: Space / Enter / Up arrow all flap.
		desk.SetOnKeyDown(func(ev *fyne.KeyEvent) {
			switch ev.Name {
			case fyne.KeySpace, fyne.KeyReturn, fyne.KeyUp:
				g.flap()
			}
		})
	}

	// Game loop at ~60 fps.
	go func() {
		tick := time.NewTicker(time.Second / 60)
		defer tick.Stop()
		for range tick.C {
			g.update()

			g.mu.Lock()
			state, score, hi := g.state, g.score, g.hiScore
			g.mu.Unlock()

			switch state {
			case StateStart:
				g.msgText.Text = "FLAPPY GOPHER"
	if fyne.CurrentDevice().IsMobile() {
		g.subText.Text = "tap to play"
	} else {
		g.subText.Text = "hit space/enter to play"
	}
	
				g.msgText.Show()
				g.subText.Show()
			case StatePlaying:
				g.scoreText.Text = fmt.Sprintf("%d", score)
				textBox.Show()
				g.msgText.Hide()
				g.subText.Hide()
			case StateOver:
				g.msgText.Text = "GAME OVER"
				prefix := ""
				if hi > 0 {
					prefix = fmt.Sprintf("Best: %d  •  ", hi)
				}
				g.subText.Text = fmt.Sprintf("%sScore: %d – tap to retry", prefix, score)
				g.msgText.Show()
				g.subText.Show()
			}

			g.updateOverlay()
			canvas.Refresh(g.raster)
			canvas.Refresh(g.overlay)
			canvas.Refresh(g.scoreText)
			canvas.Refresh(g.msgText)
			canvas.Refresh(g.subText)
		}
	}()

	w.ShowAndRun()
}
