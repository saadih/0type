package cleanup

import (
	"os"
	"strings"
	"testing"
)

// TestLLMLive talks to a running local llama-server, so it only runs on
// request: ZEROTYPE_LLM_TEST=http://127.0.0.1:8719 go test ./internal/cleanup.
func TestLLMLive(t *testing.T) {
	url := os.Getenv("ZEROTYPE_LLM_TEST")
	if url == "" {
		t.Skip("set ZEROTYPE_LLM_TEST to a llama-server base URL")
	}
	l := NewLLM(url)

	// A misheard word is repaired from context.
	got, err := l.Clean("yeah I say at the computer a lot", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(got), "i sit at the computer") {
		t.Errorf("misheard word not repaired: %q", got)
	}

	// A long transcript is chunked and reassembled without losing content.
	// Each sentence names a distinct item so the cleaner has no repetition to
	// collapse.
	var items []string
	for _, it := range []string{"login page", "export button", "search box", "dark mode toggle", "profile photo", "settings drawer", "billing tab", "invite link", "keyboard shortcuts", "notification bell", "audit log", "file uploader", "date picker", "language menu"} {
		items = append(items, "so the next thing on the list is um the "+it+" which does nothing when you click it and I think that's because the handler is never wired up.")
	}
	long := strings.Join(items, " ") // ~2,100 chars -> 2 chunks
	got, err = l.Clean(long, "")
	if err != nil {
		t.Fatal(err)
	}
	missing := 0
	for _, it := range []string{"login page", "export button", "search box", "dark mode", "profile photo", "settings drawer", "billing tab", "invite link", "keyboard shortcuts", "notification bell", "audit log", "file uploader", "date picker", "language menu"} {
		if !strings.Contains(strings.ToLower(got), it) {
			missing++
		}
	}
	if missing > 1 {
		t.Errorf("long transcript lost %d of 14 items:\n%s", missing, got)
	}

	// Context is used for continuity and never echoed back.
	prev := `For example, I say, "Yeah, I sit at the computer a lot," and it`
	got, err = l.Clean("auto transcribes to I say at the computer a lot but the LLM doesn't really catch that", prev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.ToLower(got), "for example") {
		t.Errorf("previous context echoed: %q", got)
	}
	t.Logf("continuation: %q", got)

	// The user's note steers spelling of names and terms.
	noted := NewLLM(url)
	noted.Notes = "I work at OpMore. My tools: Wails, Parakeet, llama.cpp. My colleague is Saadi."
	got, err = noted.Clean("so I built the window with whales and sadie helped with the parakeet part", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Wails") || !strings.Contains(got, "Saadi") {
		t.Errorf("notes not applied: %q", got)
	}
	t.Logf("with notes: %q", got)
}

// TestLLMLiveMidSentenceStart guards the segmented path. The segmenter cuts at
// ordinary between-phrase gaps, so a transcript usually BEGINS mid-sentence,
// carrying the words that finish the thought the previous segment left hanging.
// Those words belong to the transcript, but the model used to drop them as
// though they were part of <previous>, silently losing speech.
func TestLLMLiveMidSentenceStart(t *testing.T) {
	url := os.Getenv("ZEROTYPE_LLM_TEST")
	if url == "" {
		t.Skip("set ZEROTYPE_LLM_TEST to a llama-server base URL")
	}
	l := NewLLM(url)

	prev := "second, the export button doesn't do anything at all, and I think the handler is"
	got, err := l.Clean("never wired up and the third thing is the dark mode toggle resets every time you", prev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(got), "never wired up") {
		t.Errorf("dropped the words completing the previous sentence: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "export button") {
		t.Errorf("echoed the previous segment back: %q", got)
	}
	t.Logf("mid-sentence start: %q", got)

	// The same, in Swedish: the continuation must survive and stay Swedish.
	got, err = l.Clean("funktionen aldrig är kopplad kan du kolla på det idag",
		"och den andra är att exportknappen inte gör någonting alls, jag tror att")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(got), "funktionen aldrig är kopplad") {
		t.Errorf("Swedish continuation lost or translated: %q", got)
	}
	t.Logf("mid-sentence start (sv): %q", got)
}
