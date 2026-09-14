// Package recommend turns OpenRouter's public rankings into short model
// shortlists for the settings window: the best value, fastest, and smartest
// chat models to clean transcripts with. It refreshes itself from the feeds behind
// openrouter.ai/rankings, caches the result for a day, and falls back to an
// embedded snapshot when offline.
//
// Rankings data by OpenRouter is licensed CC BY 4.0; Result.Source carries the
// attribution line the UI shows.
package recommend

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Feeds. The first is the documented catalog; the second is the JSON the
// rankings page itself loads (undocumented, so a refresh that finds it missing
// or reshaped fails as a whole and the cache/snapshot stands).
const (
	modelsURL      = "https://openrouter.ai/api/v1/models"
	performanceURL = "https://openrouter.ai/api/frontend/v1/rankings/performance"

	cacheFile = "recommendations.json"
	maxAge    = 24 * time.Hour

	// minIntelligence keeps the fastest/value lists to models that can still
	// do the job: a model has to score at least this on the Artificial
	// Analysis intelligence index to be recommended for cleanup at all.
	minIntelligence = 25
	// minRequests keeps the cleanup lists to models with real traffic.
	minRequests = 5000
)

//go:embed snapshot.json
var snapshot []byte

// Pick is one recommended model.
type Pick struct {
	Model  string `json:"model"`  // OpenRouter slug to put in the model field
	Name   string `json:"name"`   // display name
	Detail string `json:"detail"` // the number that earned it the spot, plus price
}

// Group is one titled shortlist.
type Group struct {
	Title string `json:"title"`
	Picks []Pick `json:"picks"`
}

// Result is what the settings window shows.
type Result struct {
	AsOf    string  `json:"asOf"`   // RFC 3339; a string so the Wails binding generator can type it
	Source  string  `json:"source"` // CC BY 4.0 attribution line
	Cleanup []Group `json:"cleanup"`
	// Stale is set when a refresh failed and this came from the cache or the
	// embedded snapshot instead.
	Stale bool `json:"stale"`
}

// Get returns recommendations, refreshing from OpenRouter when the cache in
// dir is older than a day (or force is set) and falling back to the cache or
// the embedded snapshot when the network is unavailable.
func Get(dir string, force bool) (Result, error) {
	path := filepath.Join(dir, cacheFile)
	cached, cacheErr := readCache(path)
	if at, err := time.Parse(time.RFC3339, cached.AsOf); cacheErr == nil && err == nil && !force && time.Since(at) < maxAge {
		return cached, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fresh, err := Fetch(ctx)
	if err == nil {
		if b, err := json.MarshalIndent(fresh, "", "  "); err == nil {
			_ = os.MkdirAll(dir, 0o755)
			_ = os.WriteFile(path, b, 0o644)
		}
		return fresh, nil
	}
	if cacheErr == nil {
		cached.Stale = true
		return cached, nil
	}
	var snap Result
	if jerr := json.Unmarshal(snapshot, &snap); jerr != nil {
		return Result{}, fmt.Errorf("recommendations: %w (and no snapshot: %v)", err, jerr)
	}
	snap.Stale = true
	return snap, nil
}

func readCache(path string) (Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		return Result{}, err
	}
	return r, nil
}

// Catalog rows from /api/v1/models.
type catalogModel struct {
	ID            string `json:"id"`
	CanonicalSlug string `json:"canonical_slug"`
	Name          string `json:"name"`
	Architecture  struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
	Benchmarks struct {
		ArtificialAnalysis *struct {
			Intelligence float64 `json:"intelligence_index"`
		} `json:"artificial_analysis"`
	} `json:"benchmarks"`
}

// Performance rows: p50 latency (ms) and throughput (tok/s) per model.
type perfModel struct {
	Slug         string  `json:"slug"`
	RequestCount int64   `json:"request_count"`
	Latency      float64 `json:"p50_latency"`
	Throughput   float64 `json:"p50_throughput"`
}

// Fetch pulls the feeds and computes fresh recommendations.
func Fetch(ctx context.Context) (Result, error) {
	var (
		wg      sync.WaitGroup
		catalog struct {
			Data []catalogModel `json:"data"`
		}
		perf struct {
			Data []perfModel `json:"data"`
		}
		catErr, perfErr error
	)
	wg.Add(2)
	go func() { defer wg.Done(); catErr = getJSON(ctx, modelsURL, &catalog) }()
	go func() { defer wg.Done(); perfErr = getJSON(ctx, performanceURL, &perf) }()
	wg.Wait()
	// Either feed missing means a half-empty result; better to keep serving the
	// last complete one than to cache a hole for a day.
	for _, err := range []error{catErr, perfErr} {
		if err != nil {
			return Result{}, err
		}
	}
	r := Build(catalog.Data, perf.Data)
	if len(r.Cleanup) != 3 {
		return Result{}, fmt.Errorf("rankings feeds returned no usable rows (shape changed?)")
	}
	for _, g := range r.Cleanup {
		if len(g.Picks) == 0 {
			return Result{}, fmt.Errorf("rankings feed for %q returned no usable rows", g.Title)
		}
	}
	return r, nil
}

func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "0type (+https://github.com/saadih/0type)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// candidate is a chat model with everything the cleanup lists rank on.
type candidate struct {
	id, name     string
	intelligence float64
	promptPerM   float64 // $ per million input tokens
	outPerM      float64
	latency      float64 // ms
	throughput   float64
	requests     int64
}

func (c candidate) price() string {
	return fmt.Sprintf("$%s/$%s per M", trim(c.promptPerM), trim(c.outPerM))
}

func trim(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" {
		s = "0"
	}
	return s
}

// Build computes the lists from already-fetched feed rows. Exported for tests
// and the snapshot generator.
func Build(catalog []catalogModel, perf []perfModel) Result {
	now := time.Now().UTC()
	r := Result{
		AsOf:   now.Format(time.RFC3339),
		Source: "Source: OpenRouter (openrouter.ai/rankings), as of " + now.Format("2006-01-02") + ". CC BY 4.0.",
	}

	// Index the catalog by both slug forms: the rankings feeds use permaslugs
	// (with a date suffix), the model field needs the plain id.
	byCanon := map[string]catalogModel{}
	byID := map[string]catalogModel{}
	for _, m := range catalog {
		byID[m.ID] = m
		// Variants (":batch", ":free") share the base model's canonical slug;
		// only the plain id should stand for it.
		if m.CanonicalSlug != "" && !strings.Contains(m.ID, ":") {
			byCanon[m.CanonicalSlug] = m
		}
	}
	resolve := func(slug string) (catalogModel, bool) {
		if m, ok := byCanon[slug]; ok {
			return m, true
		}
		m, ok := byID[slug]
		return m, ok
	}

	// Cleanup candidates: chat models with traffic, a benchmark score, and a price.
	var cands []candidate
	for _, p := range perf {
		m, ok := resolve(p.Slug)
		if !ok || strings.Contains(m.ID, ":") || !has(m.Architecture.OutputModalities, "text") {
			continue
		}
		if m.Benchmarks.ArtificialAnalysis == nil || p.RequestCount < minRequests {
			continue
		}
		in, _ := strconv.ParseFloat(m.Pricing.Prompt, 64)
		out, _ := strconv.ParseFloat(m.Pricing.Completion, 64)
		if in <= 0 || out <= 0 {
			continue
		}
		cands = append(cands, candidate{
			id: m.ID, name: m.Name,
			intelligence: m.Benchmarks.ArtificialAnalysis.Intelligence,
			promptPerM:   in * 1e6, outPerM: out * 1e6,
			latency: p.Latency, throughput: p.Throughput, requests: p.RequestCount,
		})
	}
	if len(cands) > 0 {
		r.Cleanup = []Group{
			{Title: "Best value", Picks: top3(cands, func(c candidate) bool { return c.intelligence >= minIntelligence },
				func(a, b candidate) bool { return value(a) > value(b) },
				func(c candidate) string { return fmt.Sprintf("intelligence %.0f · %s", c.intelligence, c.price()) })},
			{Title: "Fastest", Picks: top3(cands, func(c candidate) bool { return c.intelligence >= minIntelligence && c.latency > 0 },
				func(a, b candidate) bool { return a.latency < b.latency },
				func(c candidate) string { return fmt.Sprintf("%.0f ms to first token · %s", c.latency, c.price()) })},
			{Title: "Smartest", Picks: top3(cands, func(c candidate) bool { return true },
				func(a, b candidate) bool { return a.intelligence > b.intelligence },
				func(c candidate) string { return fmt.Sprintf("intelligence %.1f · %s", c.intelligence, c.price()) })},
		}
	}

	return r
}

// value is intelligence per dollar of blended (half input, half output) price.
func value(c candidate) float64 {
	return c.intelligence / ((c.promptPerM + c.outPerM) / 2)
}

func top3(cands []candidate, keep func(candidate) bool, less func(a, b candidate) bool, detail func(candidate) string) []Pick {
	var xs []candidate
	for _, c := range cands {
		if keep(c) {
			xs = append(xs, c)
		}
	}
	sort.SliceStable(xs, func(i, j int) bool { return less(xs[i], xs[j]) })
	var out []Pick
	for _, c := range xs {
		if len(out) == 3 {
			break
		}
		out = append(out, Pick{Model: c.id, Name: c.name, Detail: detail(c)})
	}
	return out
}

func has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
