package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"worddata/internal/model"
	"worddata/internal/store"
)

// 嵌入未使用的依赖，测试只替换当前接口涉及的方法。
type fakeStore struct {
	*store.Store
	card                                            *model.UserWord
	ownerErr                                        error
	ownerCalls, reviewCalls, lookupCalls, saveCalls int
	gotID, gotOwner                                 int64
	gotQuality                                      int
	gotText                                         string
}

func (f *fakeStore) GetUserWordByIDAndUser(ctx context.Context, id, owner int64) (*model.UserWord, error) {
	f.ownerCalls++
	f.gotID, f.gotOwner = id, owner
	if f.ownerErr != nil {
		return nil, f.ownerErr
	}
	if f.card != nil && f.card.ID == id && f.card.TgUserID == owner {
		return f.card, nil
	}
	return nil, nil
}

func (f *fakeStore) SubmitReview(ctx context.Context, id int64, quality int) (*model.UserWord, error) {
	f.reviewCalls++
	f.gotID, f.gotQuality = id, quality
	return f.card, nil
}

func (f *fakeStore) GetWordByText(ctx context.Context, text string) (*model.Word, error) {
	f.lookupCalls++
	f.gotText = text
	return nil, nil
}

func (f *fakeStore) SaveWord(ctx context.Context, word *model.Word) (*model.Word, error) {
	f.saveCalls++
	word.ID = 42
	return word, nil
}

type fakeDictionary struct{ calls int }

func (f *fakeDictionary) Fetch(ctx context.Context, text string) (*model.Word, error) {
	f.calls++
	return &model.Word{Text: text, Senses: []model.Sense{{MeaningEN: "definition"}}}, nil
}

func TestReviewOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, owner                         string
		wantStatus, wantChecks, wantReviews int
	}{
		{"匹配", ", \"tg_user_id\":123", 200, 1, 1},
		{"不匹配", ", \"tg_user_id\":456", 404, 1, 0},
		{"显式零值也校验", ", \"tg_user_id\":0", 404, 1, 0},
		{"未传保持兼容", "", 200, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &fakeStore{card: &model.UserWord{ID: 9, TgUserID: 123, WordID: 8, EaseFactor: 2.5, DueAt: "2026-11-11", Status: 1}}
			rec := httptest.NewRecorder()
			New(st, &fakeDictionary{}).Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/reviews", strings.NewReader(`{"user_word_id":9,"quality":3`+tc.owner+"}")))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if st.ownerCalls != tc.wantChecks || st.reviewCalls != tc.wantReviews {
				t.Fatalf("checks/reviews = %d/%d", st.ownerCalls, st.reviewCalls)
			}
			if st.gotID != 9 {
				t.Fatalf("user_word_id = %d", st.gotID)
			}
			if tc.wantChecks == 1 && tc.name == "匹配" && st.gotOwner != 123 {
				t.Fatalf("tg_user_id = %d", st.gotOwner)
			}
			if tc.wantReviews == 1 {
				var got model.UserWord
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(&got, st.card) {
					t.Fatalf("response = %+v", got)
				}
			}
		})
	}
}

func TestReviewMissingAndStoreError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"不存在", nil, 404},
		{"查询失败保持500", errors.New("store unavailable"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &fakeStore{ownerErr: tc.err}
			rec := httptest.NewRecorder()
			New(st, &fakeDictionary{}).Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/reviews", strings.NewReader(`{"user_word_id":9,"tg_user_id":123,"quality":3}`)))
			if rec.Code != tc.status || st.reviewCalls != 0 {
				t.Fatalf("status/reviews = %d/%d", rec.Code, st.reviewCalls)
			}
		})
	}
}

func TestReviewQuality(t *testing.T) {
	for _, quality := range []int{0, 7, -1, 1, 4, 6, 100, 2, 3, 5, 99} {
		t.Run(fmt.Sprint(quality), func(t *testing.T) {
			st := &fakeStore{card: &model.UserWord{ID: 9, TgUserID: 123}}
			rec := httptest.NewRecorder()
			body := fmt.Sprintf(`{"user_word_id":9,"tg_user_id":123,"quality":%d}`, quality)
			New(st, &fakeDictionary{}).Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/reviews", strings.NewReader(body)))
			switch quality {
			case 2, 3, 5, 99:
				if rec.Code != 200 || st.reviewCalls != 1 || st.gotQuality != quality {
					t.Fatalf("status/reviews/quality = %d/%d/%d", rec.Code, st.reviewCalls, st.gotQuality)
				}
			default:
				if rec.Code != 400 || st.reviewCalls != 0 || st.ownerCalls != 0 {
					t.Fatalf("status/reviews/checks = %d/%d/%d", rec.Code, st.reviewCalls, st.ownerCalls)
				}
			}
		})
	}
}

func TestLookupWhitelist(t *testing.T) {
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{"a", true}, {"Apple", true}, {"well-being", true}, {"don't", true},
		{strings.Repeat("a", 64), true},
		{"中文", false}, {"two words", false}, {" word ", false},
		{strings.Repeat("a", 65), false}, {"foo/bar", false},
		{"https://example.com", false}, {"word1", false},
		{"-word", false}, {"word-", false}, {"'word", false}, {"word'", false},
		{"well--being", false}, {"don’t", false}, {"word\n", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			st, dc := &fakeStore{}, &fakeDictionary{}
			rec := httptest.NewRecorder()
			New(st, dc).Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/words/"+url.PathEscape(tc.text), nil))
			if !tc.valid {
				if rec.Code != 400 || dc.calls != 0 || st.lookupCalls != 0 || st.saveCalls != 0 {
					t.Fatalf("status/fetch/lookup/save = %d/%d/%d/%d", rec.Code, dc.calls, st.lookupCalls, st.saveCalls)
				}
				return
			}
			if rec.Code != 200 || dc.calls != 1 || st.lookupCalls != 1 || st.saveCalls != 1 {
				t.Fatalf("status/fetch/lookup/save = %d/%d/%d/%d", rec.Code, dc.calls, st.lookupCalls, st.saveCalls)
			}
			var word model.Word
			if err := json.Unmarshal(rec.Body.Bytes(), &word); err != nil {
				t.Fatal(err)
			}
			if word.ID != 42 || word.Text != strings.ToLower(tc.text) || st.gotText != word.Text {
				t.Fatalf("word = %+v, lookup = %q", word, st.gotText)
			}
		})
	}
}

func TestValidWord(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"", false}, {"a", true}, {strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false}, {"well-being", true}, {"don't", true},
		{"a-b'c", true}, {"中文", false}, {"a/b", false}, {" a", false},
		{"a ", false}, {"a\n", false}, {"a--b", false}, {"-a", false}, {"a-", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			if got := validWord(tc.text); got != tc.want {
				t.Fatalf("validWord(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
