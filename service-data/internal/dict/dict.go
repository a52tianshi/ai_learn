// Package dict fetches word definitions from the free DictionaryAPI.dev,
// Youdao Suggest API, or Gemini API. It parses the response into model.Word.
package dict

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"worddata/internal/model"
)

// ErrNotFound means the dictionary has no entry for the word (HTTP 404).
var ErrNotFound = errors.New("word not found in dictionary")

// Client talks to DictionaryAPI.dev, Youdao, or Gemini.
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

type youdaoSuggest struct {
	Result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	} `json:"result"`
	Data struct {
		Entries []struct {
			Entry   string `json:"entry"`
			Explain string `json:"explain"`
		} `json:"entries"`
	} `json:"data"`
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
		w, err := c.fetchGemini(ctx, text)
		if err == nil {
			return w, nil
		}
		log.Printf("Gemini lookup failed (%v), falling back to free bilingual dictionary...", err)
	}
	return c.fetchBilingualFree(ctx, text)
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

func (c *Client) fetchBilingualFree(ctx context.Context, text string) (*model.Word, error) {
	// 1. Fetch English meanings
	w, err := c.fetchDictionaryAPI(ctx, text)

	// 2. Fetch Youdao Chinese explanation
	youdaoExplain := c.fetchYoudaoExplain(ctx, text)

	if err != nil {
		// If DictionaryAPI returned 404 but Youdao has it, build a fallback word entry
		if errors.Is(err, ErrNotFound) && youdaoExplain != "" {
			return c.fetchYoudaoWord(ctx, text, youdaoExplain)
		}
		return nil, err
	}

	// 3. Merge Chinese explanation into the senses
	if youdaoExplain != "" {
		for i := range w.Senses {
			w.Senses[i].MeaningCN = extractPOSMeaning(youdaoExplain, w.Senses[i].POS)
		}
	}

	return w, nil
}

func (c *Client) fetchYoudaoExplain(ctx context.Context, text string) string {
	u := "https://dict.youdao.com/suggest?q=" + url.QueryEscape(text) + "&num=1&doctype=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var sug youdaoSuggest
	if err := json.NewDecoder(resp.Body).Decode(&sug); err != nil {
		return ""
	}

	for _, entry := range sug.Data.Entries {
		if strings.ToLower(entry.Entry) == strings.ToLower(text) {
			return entry.Explain
		}
	}
	return ""
}

func (c *Client) fetchYoudaoWord(ctx context.Context, text string, explain string) (*model.Word, error) {
	w := &model.Word{
		Text: text,
	}

	allPOS := []string{"adj.", "n.", "v.", "vt.", "vi.", "adv.", "prep.", "conj.", "pron."}
	posMap := map[string]string{
		"adj.":  "adjective",
		"n.":    "noun",
		"v.":    "verb",
		"vt.":   "verb",
		"vi.":   "verb",
		"adv.":  "adverb",
		"prep.": "preposition",
		"conj.": "conjunction",
		"pron.": "pronoun",
	}

	foundSenses := false
	for _, marker := range allPOS {
		if strings.Contains(explain, marker) {
			meaning := extractPOSMeaning(explain, posMap[marker])
			if meaning != "" {
				w.Senses = append(w.Senses, model.Sense{
					POS:       posMap[marker],
					MeaningEN: meaning, // Duplicate to EN definition so bot has text to display
					MeaningCN: meaning,
				})
				foundSenses = true
			}
		}
	}

	if !foundSenses {
		w.Senses = append(w.Senses, model.Sense{
			MeaningEN: explain,
			MeaningCN: explain,
		})
	}

	return w, nil
}

func extractPOSMeaning(explain string, pos string) string {
	explain = strings.TrimSpace(explain)
	if explain == "" {
		return ""
	}

	var prefixes []string
	switch strings.ToLower(pos) {
	case "noun":
		prefixes = []string{"n."}
	case "verb":
		prefixes = []string{"v.", "vt.", "vi."}
	case "adjective", "adj":
		prefixes = []string{"adj."}
	case "adverb", "adv":
		prefixes = []string{"adv."}
	case "preposition", "prep":
		prefixes = []string{"prep."}
	case "conjunction", "conj":
		prefixes = []string{"conj."}
	case "pronoun", "pron":
		prefixes = []string{"pron."}
	}

	if len(prefixes) == 0 {
		return explain
	}

	for _, pref := range prefixes {
		idx := strings.Index(explain, pref)
		if idx != -1 {
			start := idx + len(pref)
			allPOSMarkers := []string{"n.", "v.", "vt.", "vi.", "adj.", "adv.", "prep.", "conj.", "pron."}
			end := len(explain)
			for _, marker := range allPOSMarkers {
				mIdx := strings.Index(explain[start:], marker)
				if mIdx != -1 {
					mIdx += start
					if mIdx < end {
						end = mIdx
					}
				}
			}
			meaning := strings.Trim(explain[start:end], " \t\r\n;；,，")
			if meaning != "" {
				return meaning
			}
		}
	}

	return explain
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
