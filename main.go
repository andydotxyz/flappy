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
	"fyne.io/fyne/v2/container"
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
	prefs     fyne.Preferences

	raster    *canvas.Raster
	scoreText *canvas.Text
	msgText   *canvas.Text
	subText   *canvas.Text
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

	g.subText = canvas.NewText("SPACE / tap to play", color.RGBA{R: 255, G: 240, B: 80, A: 255})
	g.subText.TextSize = 18
	g.subText.Alignment = fyne.TextAlignCenter

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
	case StatePlaying:
		g.birdVY = flapForce
		g.flapTimer = 20
	case StateOver:
		if g.score > g.hiScore {
			g.hiScore = g.score
			g.prefs.SetInt("hiScore", g.hiScore)
		}
		g.birdY = gameH / 2
		g.birdVY = 0
		g.pipes = nil
		g.score = 0
		g.frame = 0
		g.flapTimer = 0
		g.state = StateStart
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
		return
	}
	// Pipe collision.
	for _, p := range g.pipes {
		if pipeCollides(g.birdY, p) {
			g.state = StateOver
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
	by := g.birdY
	bvy := g.birdVY
	ft := g.flapTimer
	pipes := make([]Pipe, len(g.pipes))
	copy(pipes, g.pipes)
	g.mu.Unlock()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	sx, sy := float64(w)/gameW, float64(h)/gameH
	gndY := int((gameH - groundH) * sy)

	// Sky gradient (light→darker blue going down).
	for y := 0; y < gndY; y++ {
		t := float64(y) / float64(gndY)
		r, gg, b := clamp8(lerp(135, 80, t)), clamp8(lerp(206, 160, t)), clamp8(lerp(235, 210, t))
		off := y * img.Stride
		for x := 0; x < w; x++ {
			img.Pix[off+x*4], img.Pix[off+x*4+1], img.Pix[off+x*4+2], img.Pix[off+x*4+3] = r, gg, b, 255
		}
	}

	// Ground (grass strip + dirt).
	grassH := max(int(6*sy), 3)
	for y := gndY; y < h; y++ {
		var r, gg, b uint8
		if y < gndY+grassH {
			r, gg, b = 88, 155, 60
		} else {
			r, gg, b = 210, 180, 120
		}
		off := y * img.Stride
		for x := 0; x < w; x++ {
			img.Pix[off+x*4], img.Pix[off+x*4+1], img.Pix[off+x*4+2], img.Pix[off+x*4+3] = r, gg, b, 255
		}
	}

	// Pipes.
	for _, p := range pipes {
		drawPipe(img, p, sx, sy, gndY, w, h)
	}

	// Bird.
	bx := int(birdX * sx)
	byPx := int(by * sy)
	br := max(int(birdRadius*math.Min(sx, sy)), 8)
	drawBird(img, bx, byPx, br, bvy, ft, w, h)

	return img
}

func lerp(a, b uint8, t float64) float64 { return float64(a)*(1-t) + float64(b)*t }
func clamp8(v float64) uint8              { return uint8(v) }

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

func (t *tapWidget) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	a := app.NewWithID("xyz.andy.flappy-gopher")
	w := a.NewWindow("Flappy Gopher")

	g := NewGame(a.Preferences())

	// Layer stack: raster → tap catcher → score (top border) → messages (center).
	content := container.NewStack(
		g.raster,
		newTapWidget(g.flap),
		container.NewBorder(container.NewCenter(g.scoreText), nil, nil, nil),
		container.NewCenter(container.NewVBox(g.msgText, g.subText)),
	)

	w.SetContent(content)
	w.Resize(fyne.NewSize(gameW, gameH))
	w.SetFixedSize(true)

	// Keyboard: Space / Enter / Up arrow all flap.
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		switch ev.Name {
		case fyne.KeySpace, fyne.KeyReturn, fyne.KeyUp:
			g.flap()
		}
	})

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
				g.subText.Text = "SPACE / tap to play"
				g.msgText.Show()
				g.subText.Show()
				g.scoreText.Hide()
			case StatePlaying:
				g.scoreText.Text = fmt.Sprintf("%d", score)
				g.scoreText.Show()
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
				g.scoreText.Hide()
			}

			canvas.Refresh(g.raster)
			canvas.Refresh(g.scoreText)
			canvas.Refresh(g.msgText)
			canvas.Refresh(g.subText)
		}
	}()

	w.ShowAndRun()
}
