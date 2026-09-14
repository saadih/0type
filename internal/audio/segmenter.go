package audio

import (
	"math"
	"time"
)

// Segmenter splits a live PCM stream into utterances at natural pauses, so a
// long dictation can be transcribed and pasted piece by piece while the speaker
// keeps talking. Feed it the chunks a Streaming recorder delivers; it calls
// emit with each finished segment (raw PCM, same format) and tracks how far
// into the stream it has cut, so the caller can transcribe the remainder when
// the recording stops.
//
// Speech is detected by frame energy against an adaptive noise floor. It is a
// deliberately simple gate: the goal is to find pauses long enough to be a
// phrase boundary, not to classify every frame perfectly.
type Segmenter struct {
	// MinPause is the silence that ends a segment once speech has been heard.
	MinPause time.Duration
	// MinSegment keeps very short segments from being cut; audio shorter than
	// this waits for more.
	MinSegment time.Duration
	// SoftMax is the length after which any short pause (ShortPause) is taken
	// as a boundary, to keep segments a size the transcriber handles quickly.
	SoftMax time.Duration
	// ShortPause is the pause accepted once SoftMax is exceeded.
	ShortPause time.Duration
	// HardMax cuts regardless of pauses, so one segment never grows unbounded.
	HardMax time.Duration

	pending  []byte // audio since the last cut
	analyzed int    // bytes of pending already run through the gate
	speech   bool   // pending contains speech
	silence  int    // trailing silent bytes in pending
	cut      int    // bytes of the stream emitted as segments so far
	floor    float64
}

const (
	frameBytes   = BytesPerSecond / 50 // 20 ms frames
	minThreshold = 300.0               // RMS (16-bit units) below which nothing counts as speech
	maxThreshold = 3000.0
	initialFloor = 150.0 // RMS of a quiet room, before any frame has been seen
)

// NewSegmenter returns a segmenter with defaults tuned for dictation latency.
//
// What the speaker feels is the gap between releasing the trigger and the last
// text landing, and that gap is the leftover tail: whatever has piled up since
// the previous cut. SoftMax bounds it, so it is deliberately short — past 7 s
// any ordinary between-phrase gap ends the segment, which puts the expected
// tail near 3-4 s (well under a second of transcription and cleanup). MinPause
// is the unhurried case: a clear 0.5 s pause is a phrase boundary whatever the
// length. HardMax only fires for speech with no gap at all.
func NewSegmenter() *Segmenter {
	return &Segmenter{
		MinPause:   500 * time.Millisecond,
		MinSegment: 1000 * time.Millisecond,
		SoftMax:    7 * time.Second,
		ShortPause: 200 * time.Millisecond,
		HardMax:    20 * time.Second,
	}
}

// Reset clears all state for a new recording.
func (s *Segmenter) Reset() {
	s.pending = s.pending[:0]
	s.analyzed = 0
	s.speech = false
	s.silence = 0
	s.cut = 0
	s.floor = 0
}

// Offset is the number of stream bytes emitted as segments so far: the point
// from which the remainder of the recording still needs transcribing.
func (s *Segmenter) Offset() int { return s.cut }

// TailHasSpeech reports whether speech was heard after the last cut.
func (s *Segmenter) TailHasSpeech() bool { return s.speech }

// Tail returns a copy of the audio since the last cut: what still needs
// transcribing when the recording stops.
func (s *Segmenter) Tail() []byte {
	out := make([]byte, len(s.pending))
	copy(out, s.pending)
	return out
}

// Push feeds new PCM and emits any segments that complete. emit may be nil.
func (s *Segmenter) Push(pcm []byte, emit func(seg []byte)) {
	s.pending = append(s.pending, pcm...)
	for s.analyzed+frameBytes <= len(s.pending) {
		frame := s.pending[s.analyzed : s.analyzed+frameBytes]
		s.analyzed += frameBytes
		if s.isSpeech(frameRMS(frame)) {
			s.speech = true
			s.silence = 0
		} else {
			s.silence += frameBytes
		}
		if s.shouldCut() {
			s.flush(emit)
		}
	}
}

func bytesFor(d time.Duration) int {
	return int(d.Seconds()*BytesPerSecond) / frameBytes * frameBytes
}

func (s *Segmenter) shouldCut() bool {
	n := s.analyzed
	switch {
	case !s.speech:
		// Nothing but silence so far: drop it once it gets long, rather than
		// carrying seconds of dead air into the first real segment.
		return n >= bytesFor(s.SoftMax)
	case n >= bytesFor(s.HardMax):
		return true
	case n >= bytesFor(s.SoftMax):
		return s.silence >= bytesFor(s.ShortPause)
	default:
		return n >= bytesFor(s.MinSegment) && s.silence >= bytesFor(s.MinPause)
	}
}

// flush emits the analyzed part of pending (if it holds speech) and starts a
// new segment from whatever partial frame is left.
func (s *Segmenter) flush(emit func([]byte)) {
	if s.speech && emit != nil {
		seg := make([]byte, s.analyzed)
		copy(seg, s.pending[:s.analyzed])
		emit(seg)
	}
	s.cut += s.analyzed
	rest := copy(s.pending, s.pending[s.analyzed:])
	s.pending = s.pending[:rest]
	s.analyzed = 0
	s.speech = false
	s.silence = 0
}

// isSpeech gates one frame's RMS against an adaptive noise floor. The floor
// starts at a quiet-room guess, drops instantly to any quieter frame, and
// otherwise creeps up slowly (about 5% per second), so a speaker who starts
// talking immediately is heard, while a steady loud background is eventually
// classified as silence instead of pinning the segmenter open.
func (s *Segmenter) isSpeech(rms float64) bool {
	if s.floor == 0 {
		s.floor = initialFloor
	}
	if rms < s.floor {
		s.floor = math.Max(rms, 1)
	} else if s.floor < maxThreshold/3 {
		s.floor *= 1.001
	}
	thr := math.Min(math.Max(3*s.floor, minThreshold), maxThreshold)
	return rms > thr
}

// frameRMS is the root-mean-square amplitude of a 16-bit little-endian frame.
func frameRMS(frame []byte) float64 {
	var sum float64
	n := len(frame) / 2
	if n == 0 {
		return 0
	}
	for i := 0; i < n; i++ {
		v := float64(int16(uint16(frame[2*i]) | uint16(frame[2*i+1])<<8))
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}
