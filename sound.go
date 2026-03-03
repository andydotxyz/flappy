package main

import (
	"math"
	"math/rand"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
)

const audioRate beep.SampleRate = 44100

var audioOK bool

func initAudio() {
	err := speaker.Init(audioRate, audioRate.N(50*time.Millisecond))
	audioOK = err == nil
}

// playFlap plays a short ascending chirp – subtle, called on every flap.
func playFlap() {
	if !audioOK {
		return
	}
	speaker.Play(chirpStream(160, 420, 0.075, 0.17))
}

// playSplat plays a low thud + noise burst on collision.
func playSplat() {
	if !audioOK {
		return
	}
	speaker.Play(splatStream())
}

// playJingle plays a short punchy jingle when a game round starts.
// Three quick staccato notes then a spring-pitched "boing" accent. (~270 ms total)
func playJingle() {
	if !audioOK {
		return
	}
	gap := beep.Silence(audioRate.N(10 * time.Millisecond))
	speaker.Play(beep.Seq(
		noteStream(392.00, 0.04, 0.30), gap, // G4 – staccato
		noteStream(523.25, 0.04, 0.30), gap, // C5 – staccato
		noteStream(783.99, 0.04, 0.30), gap, // G5 – staccato
		boingNote(1046.5, 0.12, 0.36),       // C6 – springs in from sharp
	))
}

// chirpStream generates a short sine-sweep tone (freqStart → freqEnd).
func chirpStream(freqStart, freqEnd, dur, vol float64) beep.Streamer {
	total := int(float64(audioRate) * dur)
	i := 0
	return beep.StreamerFunc(func(samples [][2]float64) (n int, ok bool) {
		for j := range samples {
			if i >= total {
				return j, false
			}
			t := float64(i) / float64(total)
			freq := freqStart + (freqEnd-freqStart)*t
			env := math.Sin(math.Pi * t) // smooth bell 0→1→0
			phase := 2 * math.Pi * freq * float64(i) / float64(audioRate)
			v := vol * env * math.Sin(phase)
			samples[j][0] = v
			samples[j][1] = v
			i++
		}
		return len(samples), true
	})
}

// splatStream generates a low thud with a decaying noise burst.
func splatStream() beep.Streamer {
	dur := 0.65
	total := int(float64(audioRate) * dur)
	i := 0
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return beep.StreamerFunc(func(samples [][2]float64) (n int, ok bool) {
		for j := range samples {
			if i >= total {
				return j, false
			}
			t := float64(i) / float64(total)
			decay := math.Exp(-t * 5)
			thump := 0.38 * math.Sin(2*math.Pi*85*float64(i)/float64(audioRate)) * decay
			noise := 0.22 * (rng.Float64()*2 - 1) * decay
			v := thump + noise
			samples[j][0] = v
			samples[j][1] = v
			i++
		}
		return len(samples), true
	})
}

// noteStream generates a single note with a smooth bell envelope.
func noteStream(freq, dur, vol float64) beep.Streamer {
	total := int(float64(audioRate) * dur)
	i := 0
	return beep.StreamerFunc(func(samples [][2]float64) (n int, ok bool) {
		for j := range samples {
			if i >= total {
				return j, false
			}
			t := float64(i) / float64(total)
			env := math.Sin(math.Pi * t)
			phase := 2 * math.Pi * freq * float64(i) / float64(audioRate)
			v := vol * env * (math.Sin(phase) + 0.15*math.Sin(2*phase))
			samples[j][0] = v
			samples[j][1] = v
			i++
		}
		return len(samples), true
	})
}

// boingNote plays a note whose pitch springs from ~15% sharp and settles to freq,
// giving a playful "boing" feel on the accent note of the jingle.
func boingNote(freq, dur, vol float64) beep.Streamer {
	total := int(float64(audioRate) * dur)
	i := 0
	return beep.StreamerFunc(func(samples [][2]float64) (n int, ok bool) {
		for j := range samples {
			if i >= total {
				return j, false
			}
			t := float64(i) / float64(total)
			f := freq * (1.0 + 0.15*math.Exp(-t*14)) // pitch decays from +15% to target
			env := math.Sin(math.Pi * t)
			phase := 2 * math.Pi * f * float64(i) / float64(audioRate)
			v := vol * env * (math.Sin(phase) + 0.15*math.Sin(2*phase))
			samples[j][0] = v
			samples[j][1] = v
			i++
		}
		return len(samples), true
	})
}
