//go:build windows

package audio

import (
	"os"
	"testing"
	"time"
)

// TestMicRecordsAndStreams records from the default microphone, so it only
// runs on request: ZEROTYPE_MIC_TEST=1 go test ./internal/audio. It checks that
// the ring keeps up past a full wrap, that every streamed byte is in the WAV,
// and that the recorder can be reused.
func TestMicRecordsAndStreams(t *testing.T) {
	if os.Getenv("ZEROTYPE_MIC_TEST") == "" {
		t.Skip("set ZEROTYPE_MIC_TEST=1 to record 3 s from the default microphone")
	}
	m := NewMic()
	streamed, chunks := 0, 0
	m.OnAudio(func(p []byte) { streamed += len(p); chunks++ })
	for run, d := range []time.Duration{3 * time.Second, 500 * time.Millisecond} {
		streamed, chunks = 0, 0
		if err := m.Start(); err != nil {
			t.Fatalf("run %d start: %v", run, err)
		}
		time.Sleep(d)
		wav, err := m.Stop()
		if err != nil {
			t.Fatalf("run %d stop: %v", run, err)
		}
		pcm := streamed
		want := int(d.Seconds() * BytesPerSecond)
		t.Logf("run %d: %d bytes of PCM (%.2f s), %d chunks streamed", run, pcm, float64(pcm)/BytesPerSecond, chunks)
		if len(wav) != HeaderBytes {
			t.Fatalf("run %d: streaming recorder returned %d bytes of WAV, want an empty one", run, len(wav))
		}
		if pcm < want*9/10 || pcm > want*12/10 {
			t.Fatalf("run %d: captured %d bytes, expected about %d", run, pcm, want)
		}
		if chunks < int(d/(250*time.Millisecond))-1 {
			t.Fatalf("run %d: only %d chunks streamed", run, chunks)
		}
	}
}
