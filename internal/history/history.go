package history

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry represents a single posted tweet in the history log.
type Entry struct {
	ID       string    `json:"id"`
	Text     string    `json:"text"`
	Category string    `json:"category"`
	HasGIF   bool      `json:"has_gif"`
	PostedAt time.Time `json:"posted_at"`
}

// Store persists tweet history to a JSON file and provides
// similarity checking to avoid posting duplicate content.
type Store struct {
	path       string
	maxEntries int
	threshold  float64
	entries    []Entry
	mu         sync.Mutex
}

// NewStore creates a history store backed by the file at path.
// maxEntries controls how many recent entries are retained.
// threshold sets the Jaccard similarity cutoff (0.0 to 1.0) above
// which a new tweet is considered too similar to a past one.
func NewStore(path string, maxEntries int, threshold float64) (*Store, error) {
	s := &Store{
		path:       path,
		maxEntries: maxEntries,
		threshold:  threshold,
	}

	if err := s.load(); err != nil {
		return nil, err
	}

	slog.Info("history store initialized",
		"path", path,
		"existing_entries", len(s.entries),
		"max_entries", maxEntries,
		"threshold", threshold,
	)

	return s, nil
}

// load reads the JSON history file into memory. If the file does not
// exist, the store starts with an empty entry list.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.entries = []Entry{}
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &s.entries); err != nil {
		return err
	}

	return nil
}

// save writes the current entries to disk atomically by writing to a
// temporary file first, then renaming it over the target path.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := s.path + ".tmp"

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}

	return nil
}

// IsTooSimilar returns true if text has a Jaccard similarity score
// at or above the configured threshold compared to any existing entry.
func (s *Store) IsTooSimilar(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range s.entries {
		score := jaccardSimilarity(text, entry.Text)
		if score >= s.threshold {
			slog.Warn("tweet too similar to existing entry",
				"existing_id", entry.ID,
				"similarity", score,
				"threshold", s.threshold,
			)
			return true
		}
	}

	return false
}

// Add appends a new entry to the history, trims to maxEntries (keeping
// the most recent), and persists to disk.
func (s *Store) Add(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, entry)

	if len(s.entries) > s.maxEntries {
		s.entries = s.entries[len(s.entries)-s.maxEntries:]
	}

	if err := s.save(); err != nil {
		slog.Error("failed to save history", "error", err)
		return err
	}

	slog.Info("entry added to history",
		"id", entry.ID,
		"category", entry.Category,
		"total_entries", len(s.entries),
	)

	return nil
}

// jaccardSimilarity computes the Jaccard index between two strings
// by treating each as a set of lowercase words.
// Result is |intersection| / |union|, ranging from 0.0 to 1.0.
func jaccardSimilarity(a, b string) float64 {
	setA := wordSet(a)
	setB := wordSet(b)

	if len(setA) == 0 && len(setB) == 0 {
		return 1.0
	}

	intersectionSize := 0
	for word := range setA {
		if _, ok := setB[word]; ok {
			intersectionSize++
		}
	}

	unionSize := len(setA)
	for word := range setB {
		if _, ok := setA[word]; !ok {
			unionSize++
		}
	}

	if unionSize == 0 {
		return 0.0
	}

	return float64(intersectionSize) / float64(unionSize)
}

// wordSet splits a string on whitespace and returns a set of
// lowercased words.
func wordSet(s string) map[string]struct{} {
	words := strings.Fields(s)
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		set[strings.ToLower(w)] = struct{}{}
	}
	return set
}
