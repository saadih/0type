package audio

import (
	"math"
	"testing"
	"time"
)

// tone returns d of a 440 Hz sine at the given 16-bit amplitude.
func tone(d time.Duration, amp float64) []byte {
	n := int(d.Seconds() * SampleRate)
	out := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		v := int16(amp * math.Sin(2*math.Pi*440*float64(i)/SampleRate))
		out[2*i] = byte(v)
		out[2*i+1] = byte(v >> 8)
	}
	return out
}

func silence(d time.Duration) []byte { return tone(d, 40) }

func feed(s *Segmenter, pcm []byte, chunk int, emit func([]byte)) {
	for len(pcm) > 0 {
		n := chunk
		if n > len(pcm) {
			n = len(pcm)
		}
		s.Push(pcm[:n], emit)
		pcm = pcm[n:]
	}
}

func TestSegmenterCutsAtPause(t *testing.T) {
	s := NewSegmenter()
	var segs [][]byte
	emit := func(b []byte) { segs = append(segs, b) }
	var stream []byte
	stream = append(stream, silence(300*time.Millisecond)...)
	stream = append(stream, tone(2*time.Second, 4000)...)
	stream = append(stream, silence(1200*time.Millisecond)...)
	stream = append(stream, tone(1500*time.Millisecond, 4000)...)
	stream = append(stream, silence(1200*time.Millisecond)...)
	feed(s, stream, BytesPerSecond/4, emit)
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2", len(segs))
	}
	if s.TailHasSpeech() {
		t.Fatalf("tail should be silent")
	}
	total := 0
	for _, seg := range segs {
		total += len(seg)
	}
	if s.Offset() != total {
		t.Fatalf("offset %d != emitted %d", s.Offset(), total)
	}
	if s.Offset() > len(stream) {
		t.Fatalf("offset past stream")
	}
}

func TestSegmenterNoCutWhileTalking(t *testing.T) {
	s := NewSegmenter()
	n := 0
	feed(s, tone(10*time.Second, 3000), BytesPerSecond/4, func([]byte) { n++ })
	if n != 0 {
		t.Fatalf("cut %d times during continuous speech", n)
	}
	if !s.TailHasSpeech() {
		t.Fatalf("tail should have speech")
	}
}

func TestSegmenterHardMax(t *testing.T) {
	s := NewSegmenter()
	n := 0
	feed(s, tone(100*time.Second, 6000), BytesPerSecond/4, func([]byte) { n++ })
	if n != 2 {
		t.Fatalf("got %d hard cuts in 100 s, want 2", n)
	}
}

func TestSegmenterSpeechAfterLongSilence(t *testing.T) {
	// A speaker who waits before talking: leading silence must not poison the
	// floor, and speech must still be detected.
	s := NewSegmenter()
	var segs int
	var stream []byte
	stream = append(stream, silence(5*time.Second)...)
	stream = append(stream, tone(2*time.Second, 2500)...)
	stream = append(stream, silence(1*time.Second)...)
	feed(s, stream, 4000, func([]byte) { segs++ })
	if segs != 1 {
		t.Fatalf("got %d segments, want 1", segs)
	}
}
