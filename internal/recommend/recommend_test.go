package recommend

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestBuildRanksAndFilters(t *testing.T) {
	var catalog []catalogModel
	mk := func(id, canon, name string, intel float64, in, out string) catalogModel {
		var m catalogModel
		m.ID, m.CanonicalSlug, m.Name = id, canon, name
		m.Architecture.OutputModalities = []string{"text"}
		m.Pricing.Prompt, m.Pricing.Completion = in, out
		if intel > 0 {
			m.Benchmarks.ArtificialAnalysis = &struct {
				Intelligence float64 `json:"intelligence_index"`
			}{intel}
		}
		return m
	}
	catalog = append(catalog,
		mk("a/smart", "a/smart-2026", "Smart", 60, "0.00001", "0.00005"),
		mk("a/cheap", "a/cheap-2026", "Cheap", 30, "0.0000001", "0.0000004"),
		mk("a/fast", "a/fast-2026", "Fast", 26, "0.000001", "0.000002"),
		mk("a/dumb", "a/dumb-2026", "Dumb", 10, "0.00000005", "0.0000001"),
		mk("a/free:free", "a/free-2026", "Free", 40, "0", "0"),
		mk("a/smart:batch", "a/smart-2026", "Smart (batch)", 60, "0.000005", "0.000025"), // must not shadow a/smart
		mk("a/nobench", "a/nobench-2026", "NoBench", 0, "0.000001", "0.000001"),
		mk("s/whisper", "s/whisper", "Whisper", 0, "0.001", "0"),
		mk("s/parakeet", "s/parakeet-2026", "Parakeet", 0, "0.001", "0"),
	)
	perf := []perfModel{
		{Slug: "a/smart-2026", RequestCount: 100000, Latency: 2000, Throughput: 50},
		{Slug: "a/cheap-2026", RequestCount: 100000, Latency: 800, Throughput: 100},
		{Slug: "a/fast-2026", RequestCount: 100000, Latency: 150, Throughput: 300},
		{Slug: "a/dumb-2026", RequestCount: 100000, Latency: 100, Throughput: 500},
		{Slug: "a/free-2026", RequestCount: 100000, Latency: 100, Throughput: 500},
		{Slug: "a/nobench-2026", RequestCount: 100000, Latency: 100, Throughput: 500},
		{Slug: "a/quiet", RequestCount: 10, Latency: 1, Throughput: 1},
	}
	usage := []usageModel{
		{Permaslug: "s/parakeet-2026", Count: 500},
		{Permaslug: "s/whisper", Count: 9000},
		{Permaslug: "s/unknown", Count: 99999},
	}
	r := Build(catalog, perf, usage)
	if len(r.Cleanup) != 3 {
		t.Fatalf("cleanup groups: %d", len(r.Cleanup))
	}
	want := map[string]string{"Best value": "a/cheap", "Fastest": "a/fast", "Smartest": "a/smart"}
	for _, g := range r.Cleanup {
		if len(g.Picks) == 0 || g.Picks[0].Model != want[g.Title] {
			t.Errorf("%s: got %+v, want first %s", g.Title, g.Picks, want[g.Title])
		}
		for _, p := range g.Picks {
			switch p.Model {
			case "a/dumb", "a/free:free", "a/nobench":
				if g.Title != "Smartest" || p.Model != "a/dumb" {
					t.Errorf("%s: should not recommend %s", g.Title, p.Model)
				}
			}
		}
	}
	if len(r.Transcription) != 1 || len(r.Transcription[0].Picks) != 2 || r.Transcription[0].Picks[0].Model != "s/whisper" {
		t.Errorf("transcription: %+v", r.Transcription)
	}
}

func TestSnapshotParses(t *testing.T) {
	var r Result
	if err := json.Unmarshal(snapshot, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Cleanup) != 3 || len(r.Transcription) != 1 {
		t.Fatalf("snapshot incomplete: %d cleanup groups, %d transcription groups", len(r.Cleanup), len(r.Transcription))
	}
}

// TestWriteSnapshot refreshes the embedded snapshot from the live feeds:
// ZEROTYPE_WRITE_SNAPSHOT=1 go test -run TestWriteSnapshot ./internal/recommend
func TestWriteSnapshot(t *testing.T) {
	if os.Getenv("ZEROTYPE_WRITE_SNAPSHOT") == "" {
		t.Skip("set ZEROTYPE_WRITE_SNAPSHOT=1 to fetch live rankings and rewrite snapshot.json")
	}
	r, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile("snapshot.json", append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote snapshot:\n%s", b)
}
