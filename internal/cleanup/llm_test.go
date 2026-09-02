package cleanup

import (
	"strings"
	"testing"
)

func TestSplitChunksShort(t *testing.T) {
	got := splitChunks("hello world", 100)
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestSplitChunksPrefersSentenceEnds(t *testing.T) {
	s := strings.Repeat("this is a sentence. ", 20) + "and a tail"
	chunks := splitChunks(strings.TrimSpace(s), 120)
	if len(chunks) < 3 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for i, c := range chunks[:len(chunks)-1] {
		if !strings.HasSuffix(c, ".") {
			t.Errorf("chunk %d does not end at a sentence: %q", i, c)
		}
		if len(c) > 120 {
			t.Errorf("chunk %d too long: %d", i, len(c))
		}
	}
	if strings.Join(chunks, " ") != strings.TrimSpace(s) {
		t.Fatalf("chunks do not reassemble the input")
	}
}

func TestSplitChunksLongSentence(t *testing.T) {
	s := strings.TrimSpace(strings.Repeat("word ", 100))
	chunks := splitChunks(s, 50)
	for _, c := range chunks {
		if len(c) > 50 {
			t.Errorf("chunk too long: %q", c)
		}
	}
	if strings.Join(chunks, " ") != s {
		t.Fatalf("chunks do not reassemble the input")
	}
}

func TestStripOverlap(t *testing.T) {
	cases := []struct{ prev, out, raw, want string }{
		{`For example, I say, "Yeah, I sit at the computer a lot," and it`,
			`For example, I say, "Yeah, I sit at the computer a lot," and it auto transcribes to "I sit," but the LLM misses it.`,
			"auto transcribes to I sit but the LLM misses it",
			`auto transcribes to "I sit," but the LLM misses it.`},
		{"I went to the store", "I went to the store. And then I came home.", "and then I came home", "And then I came home."},
		{"I went to the store", "and then I came home", "and then I came home", "and then I came home"},
		{"I went to the store", "I went to the store", "", ""},
		{"", "Hello there.", "hello there", "Hello there."},
		{"one two", "one two three", "one two three", "one two three"}, // too short an overlap to trust
		// The speaker really repeated the phrase: keep it.
		{"Thank you very much.", "Thank you very much, John.", "thank you very much john", "Thank you very much, John."},
	}
	for _, c := range cases {
		if got := stripOverlap(c.prev, c.out, c.raw); got != c.want {
			t.Errorf("stripOverlap(%q, %q, %q) = %q, want %q", c.prev, c.out, c.raw, got, c.want)
		}
	}
}

func TestTailWords(t *testing.T) {
	if got := tailWords("a b c d e", 3); got != "c d e" {
		t.Fatalf("got %q", got)
	}
	if got := tailWords("a b", 3); got != "a b" {
		t.Fatalf("got %q", got)
	}
}
