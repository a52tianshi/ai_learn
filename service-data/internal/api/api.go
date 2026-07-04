// Package api exposes the word service over HTTP/JSON. See TD/01-总览.md §4.3.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"worddata/internal/dict"
	"worddata/internal/model"
	"worddata/internal/store"
)

// Server bundles dependencies and implements http.Handler.
type Server struct {
	store *store.Store
	dict  *dict.Client
}

// New wires a Server.
func New(st *store.Store, dc *dict.Client) *Server {
	return &Server{store: st, dict: dc}
}

// Routes returns the HTTP mux for the service.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /words/recent", s.handleRecent) // must precede /words/{text}
	mux.HandleFunc("GET /words/{text}", s.handleLookup)
	mux.HandleFunc("POST /notebook", s.handleAddNotebook)
	mux.HandleFunc("GET /reviews/due", s.handleDue)
	mux.HandleFunc("POST /reviews", s.handleReview)
	mux.HandleFunc("POST /readings", s.handleSaveReading)
	mux.HandleFunc("GET /stats", s.handleStats)
	return logging(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /words/{text}
func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	text := normalize(r.PathValue("text"))
	if text == "" {
		writeErr(w, http.StatusBadRequest, "empty word")
		return
	}
	ctx := r.Context()

	cached, err := s.store.GetWordByText(ctx, text)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cached != nil {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	fetched, err := s.dict.Fetch(ctx, text)
	if errors.Is(err, dict.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "word not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "dictionary unavailable: "+err.Error())
		return
	}

	saved, err := s.store.SaveWord(ctx, fetched)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// POST /notebook  {tg_user_id, word_id}
func (s *Server) handleAddNotebook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TgUserID int64 `json:"tg_user_id"`
		WordID   int64 `json:"word_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.TgUserID == 0 || req.WordID == 0 {
		writeErr(w, http.StatusBadRequest, "tg_user_id and word_id required")
		return
	}
	uw, err := s.store.AddToNotebook(r.Context(), req.TgUserID, req.WordID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, uw)
}

// GET /reviews/due?tg_user_id=&limit=
func (s *Server) handleDue(w http.ResponseWriter, r *http.Request) {
	uid, ok := queryInt(w, r, "tg_user_id", true)
	if !ok {
		return
	}
	limit, _ := queryInt(w, r, "limit", false)
	cards, err := s.store.GetDueWords(r.Context(), uid, int(limit))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

// POST /reviews  {user_word_id, quality}
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserWordID int64 `json:"user_word_id"`
		Quality    int   `json:"quality"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.UserWordID == 0 {
		writeErr(w, http.StatusBadRequest, "user_word_id required")
		return
	}
	uw, err := s.store.SubmitReview(r.Context(), req.UserWordID, req.Quality)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, uw)
}

// GET /words/recent?tg_user_id=&n=
func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	uid, ok := queryInt(w, r, "tg_user_id", true)
	if !ok {
		return
	}
	n, _ := queryInt(w, r, "n", false)
	words, err := s.store.GetRecentWords(r.Context(), uid, int(n))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"words": words})
}

// POST /readings  {tg_user_id, content, target_words, model}
func (s *Server) handleSaveReading(w http.ResponseWriter, r *http.Request) {
	var req model.Reading
	if !decode(w, r, &req) {
		return
	}
	if req.TgUserID == 0 || strings.TrimSpace(req.Content) == "" {
		writeErr(w, http.StatusBadRequest, "tg_user_id and content required")
		return
	}
	saved, err := s.store.SaveReading(r.Context(), &req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// GET /stats?tg_user_id=
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	uid, ok := queryInt(w, r, "tg_user_id", true)
	if !ok {
		return
	}
	st, err := s.store.Stats(r.Context(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// --- helpers ---

func normalize(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return false
	}
	return true
}

func queryInt(w http.ResponseWriter, r *http.Request, key string, required bool) (int64, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		if required {
			writeErr(w, http.StatusBadRequest, key+" required")
			return 0, false
		}
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, key+" must be an integer")
		return 0, false
	}
	return n, true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Printf("%s %s", r.Method, r.URL.Path)
	})
}
