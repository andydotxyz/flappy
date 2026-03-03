package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"time"

	"github.com/ebitengine/oto/v3"
)

const audioRate = 44100

var (
	audioOK  bool
	audioCtx *oto.Context
)

func initAudio() {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   audioRate,
		ChannelCount: 2,
		Format:       oto.FormatFloat32LE,
	})
	if err != nil {
		return
	}
	<-ready
	audioCtx = ctx
	audioOK = true
}

// playBuf plays a pre-generated stereo float32-LE sample buffer asynchronously.
func playBuf(buf []byte) {
	p := audioCtx.NewPlayer(bytes.NewReader(buf))
	p.Play()
	go func() {
		// Keep p alive until playback finishes (prevents premature GC).
		for p.IsPlaying() {
			time.Sleep(5 * time.Millisecond)
		}
	}()
}

// genSamples generates a stereo float32-LE buffer of `total` frames using fn(i).
func genSamples(total int, fn func(i int) float64) []byte {
	buf := make([]byte, total*8) // 2 channels × 4 bytes each
	for i := 0; i < total; i++ {
		v := math.Float32bits(float32(fn(i)))
		binary.LittleEndian.PutUint32(buf[i*8:], v)
		binary.LittleEndian.PutUint32(buf[i*8+4:], v)
	}
	return buf
}

func silenceBuf(d time.Duration) []byte {
	return make([]byte, int(float64(audioRate)*d.Seconds())*8)
}

func concatBufs(bufs ...[]byte) []byte {
	var n int
	for _, b := range bufs {
		n += len(b)
	}
	out := make([]byte, n)
	pos := 0
	for _, b := range bufs {
		copy(out[pos:], b)
		pos += len(b)
	}
	return out
}

// playFlap plays a short ascending chirp – subtle, called on every flap.
func playFlap() {
	if !audioOK {
		return
	}
	freqStart, freqEnd, dur, vol := 160.0, 420.0, 0.075, 0.17
	total := int(float64(audioRate) * dur)
	playBuf(genSamples(total, func(i int) float64 {
		t := float64(i) / float64(total)
		freq := freqStart + (freqEnd-freqStart)*t
		env := math.Sin(math.Pi * t)
		phase := 2 * math.Pi * freq * float64(i) / float64(audioRate)
		return vol * env * math.Sin(phase)
	}))
}

// playSplat plays a low thud + noise burst on collision.
func playSplat() {
	if !audioOK {
		return
	}
	dur := 0.65
	total := int(float64(audioRate) * dur)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	playBuf(genSamples(total, func(i int) float64 {
		t := float64(i) / float64(total)
		decay := math.Exp(-t * 5)
		thump := 0.38 * math.Sin(2*math.Pi*85*float64(i)/float64(audioRate)) * decay
		noise := 0.22 * (rng.Float64()*2 - 1) * decay
		return thump + noise
	}))
}

// playJingle plays a short punchy jingle when a game round starts.
// Three quick staccato notes then a spring-pitched "boing" accent. (~270 ms total)
func playJingle() {
	if !audioOK {
		return
	}
	gap := silenceBuf(10 * time.Millisecond)
	playBuf(concatBufs(
		noteBuf(392.00, 0.04, 0.30), gap, // G4 – staccato
		noteBuf(523.25, 0.04, 0.30), gap, // C5 – staccato
		noteBuf(783.99, 0.04, 0.30), gap, // G5 – staccato
		boingBuf(1046.5, 0.12, 0.36),     // C6 – springs in from sharp
	))
}

func noteBuf(freq, dur, vol float64) []byte {
	total := int(float64(audioRate) * dur)
	return genSamples(total, func(i int) float64 {
		t := float64(i) / float64(total)
		env := math.Sin(math.Pi * t)
		phase := 2 * math.Pi * freq * float64(i) / float64(audioRate)
		return vol * env * (math.Sin(phase) + 0.15*math.Sin(2*phase))
	})
}

func boingBuf(freq, dur, vol float64) []byte {
	total := int(float64(audioRate) * dur)
	return genSamples(total, func(i int) float64 {
		t := float64(i) / float64(total)
		f := freq * (1.0 + 0.15*math.Exp(-t*14)) // pitch decays from +15% to target
		env := math.Sin(math.Pi * t)
		phase := 2 * math.Pi * f * float64(i) / float64(audioRate)
		return vol * env * (math.Sin(phase) + 0.15*math.Sin(2*phase))
	})
}
