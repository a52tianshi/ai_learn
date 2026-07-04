// Package dict fetches word definitions from the free DictionaryAPI.dev
// (English-English, no key). It parses the response into model.Word.
package dict

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"worddata/internal/model"
)

// ErrNotFound means the dictionary has no entry for the word (HTTP 404).
var ErrNotFound = errors.New("word not found in dictionary")

// Client talks to DictionaryAPI.dev.
type Client struct {
	base string
	http *http.Client
}

// New returns a Client. base is e.g.
// https://api.dictionaryapi.dev/api/v2/entries/en
func New(base string) *Client {
	return &Client{
		base: base,
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

// raw mirrors the DictionaryAPI.dev JSON shape (only the fields we use).
type rawEntry struct {
	Word      string `json:"word"`
	Phonetic  string `json:"phonetic"`
	Phonetics []struct {
		Text  string `json:"text"`
		Audio string `json:"audio"`
	} `json:"phonetics"`
	Meanings []struct {
		PartOfSpeech string `json:"partOfSpeech"`
		Definitions  []struct {
			Definition string   `json:"definition"`
			Example    string   `json:"example"`
			Synonyms   []string `json:"synonyms"`
			Antonyms   []string `json:"antonyms"`
		} `json:"definitions"`
		Synonyms []string `json:"synonyms"`
		Antonyms []string `json:"antonyms"`
	} `json:"meanings"`
}

// maxSenses caps how many senses we keep so cards and storage stay small.
const maxSenses = 6

// Fetch looks up text and returns a partially-filled model.Word (no ID yet).
// Returns ErrNotFound on a 404.
func (c *Client) Fetch(ctx context.Context, text string) (*model.Word, error) {
	u := c.base + "/" + url.PathEscape(text)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dictionaryapi: unexpected status %d", resp.StatusCode)
	}

	var entries []rawEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("dictionaryapi: decode: %w", err)
	}
	if len(entries) == 0 {
		return nil, ErrNotFound
	}

	w := &model.Word{Text: text}
	for _, e := range entries {
		if w.Phonetic == "" {
			w.Phonetic = firstNonEmptyPhonetic(e)
		}
		if w.AudioURL == "" {
			w.AudioURL = firstAudio(e)
		}
		for _, m := range e.Meanings {
			for _, d := range m.Definitions {
				if len(w.Senses) >= maxSenses {
					return w, nil
				}
				w.Senses = append(w.Senses, model.Sense{
					POS:       m.PartOfSpeech,
					MeaningEN: d.Definition,
					Examples:  nonEmptyList(d.Example),
					Synonyms:  pick(d.Synonyms, m.Synonyms),
					Antonyms:  pick(d.Antonyms, m.Antonyms),
				})
			}
		}
	}
	if len(w.Senses) == 0 {
		return nil, ErrNotFound
	}
	return w, nil
}

func firstNonEmptyPhonetic(e rawEntry) string {
	if e.Phonetic != "" {
		return e.Phonetic
	}
	for _, p := range e.Phonetics {
		if p.Text != "" {
			return p.Text
		}
	}
	return ""
}

func firstAudio(e rawEntry) string {
	for _, p := range e.Phonetics {
		if p.Audio != "" {
			return p.Audio
		}
	}
	return ""
}

func nonEmptyList(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// pick returns the definition-level list if present, else the meaning-level one.
func pick(primary, fallback []string) []string {
	if len(primary) > 0 {
		return primary
	}
	return fallback
}
