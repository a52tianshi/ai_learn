// Package dict fetches word definitions from the free DictionaryAPI.dev
// or Gemini API. It parses the response into model.Word.
package dict

import (
	"bytes"
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

// Client talks to DictionaryAPI.dev or Gemini.
type Client struct {
	base   string
	apiKey string
	model  string
	http   *http.Client
}

// New returns a Client. base is e.g.
// https://api.dictionaryapi.dev/api/v2/entries/en
func New(base, apiKey, modelName string) *Client {
	return &Client{
		base:   base,
		apiKey: apiKey,
		model:  modelName,
		http:   &http.Client{Timeout: 15 * time.Second},
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

const geminiSystemInstruction = `You are a professional dictionary assistant. For the English word or phrase provided by the user, you must output a single JSON object.

Format the output strictly as a JSON object matching this structure:
{
  "word": "the word or phrase, corrected for casing/spelling if it was a minor typo",
  "phonetic": "phonetic symbol (optional, e.g., /ˈæp.əl/)",
  "senses": [
    {
      "pos": "part of speech, e.g., noun, verb, adjective",
      "meaning_en": "concise, accurate English definition",
      "meaning_cn": "准确、地道的中文释义",
      "examples": ["1 or 2 natural example sentences"],
      "synonyms": ["up to 3 synonyms"],
      "antonyms": ["up to 3 antonyms"]
    }
  ]
}

If the word or phrase is completely invalid, gibberish, or cannot be found as a valid English expression, return:
{
  "word": "",
  "phonetic": "",
  "senses": []
}

Only return the JSON. No markdown backticks, no comments, no extra text.`

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	ResponseMimeType string `json:"responseMimeType"`
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Fetch looks up text and returns a partially-filled model.Word (no ID yet).
// Returns ErrNotFound on a 404.
func (c *Client) Fetch(ctx context.Context, text string) (*model.Word, error) {
	if c.apiKey != "" {
		return c.fetchGemini(ctx, text)
	}
	return c.fetchDictionaryAPI(ctx, text)
}

func (c *Client) fetchGemini(ctx context.Context, text string) (*model.Word, error) {
	modelName := c.model
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}
	u := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		url.PathEscape(modelName), url.QueryEscape(c.apiKey))

	prompt := fmt.Sprintf(`Look up the English word or phrase: "%s"`, text)
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: geminiSystemInstruction + "\n\n" + prompt},
				},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			ResponseMimeType: "application/json",
		},
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errData)
		return nil, fmt.Errorf("gemini api: status %d, error: %v", resp.StatusCode, errData)
	}

	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("gemini api decode response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini api: empty candidates/parts")
	}

	rawJSONText := geminiResp.Candidates[0].Content.Parts[0].Text

	var result struct {
		Word     string `json:"word"`
		Phonetic string `json:"phonetic"`
		Senses   []struct {
			POS       string   `json:"pos"`
			MeaningEN string   `json:"meaning_en"`
			MeaningCN string   `json:"meaning_cn"`
			Examples  []string `json:"examples"`
			Synonyms  []string `json:"synonyms"`
			Antonyms  []string `json:"antonyms"`
		} `json:"senses"`
	}

	if err := json.Unmarshal([]byte(rawJSONText), &result); err != nil {
		return nil, fmt.Errorf("gemini api: parse generated JSON: %w, raw content: %s", err, rawJSONText)
	}

	if len(result.Senses) == 0 {
		return nil, ErrNotFound
	}

	w := &model.Word{
		Text:     result.Word,
		Phonetic: result.Phonetic,
	}
	if w.Text == "" {
		w.Text = text
	}

	for _, s := range result.Senses {
		if len(w.Senses) >= maxSenses {
			break
		}
		w.Senses = append(w.Senses, model.Sense{
			POS:       s.POS,
			MeaningEN: s.MeaningEN,
			MeaningCN: s.MeaningCN,
			Examples:  s.Examples,
			Synonyms:  s.Synonyms,
			Antonyms:  s.Antonyms,
		})
	}

	return w, nil
}

func (c *Client) fetchDictionaryAPI(ctx context.Context, text string) (*model.Word, error) {
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
