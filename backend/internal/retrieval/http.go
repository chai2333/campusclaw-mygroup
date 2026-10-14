package retrieval

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"campusclaw/internal/auth"
	"campusclaw/internal/knowledge"
)

const (
	defaultLimit  = 4
	maxLimit      = 20
	maxQueryRunes = 2000
)

// HTTPService exposes the fourth-lesson retrieval and rebuild contract.
// Authentication middleware supplies the class; request fields can never
// override it.
type HTTPService struct {
	DB        *sql.DB
	Retriever Retriever
	Indexer   *Indexer
}

type HitDTO struct {
	ChunkID       int64   `json:"chunk_id"`
	MaterialID    string  `json:"material_id"`
	MaterialTitle string  `json:"material_title"`
	ChunkIndex    int     `json:"chunk_index"`
	CharStart     int     `json:"char_start"`
	CharEnd       int     `json:"char_end"`
	Excerpt       string  `json:"excerpt"`
	KeywordRank   int     `json:"keyword_rank,omitempty"`
	VectorRank    int     `json:"vector_rank,omitempty"`
	FinalRank     int     `json:"final_rank,omitempty"`
	Score         float64 `json:"score,omitempty"`
	SourceURL     string  `json:"source_url"`
}

func ToHitDTO(h Hit) HitDTO {
	return HitDTO{
		ChunkID: h.ChunkID, MaterialID: h.MaterialID,
		MaterialTitle: h.MaterialTitle, ChunkIndex: h.ChunkIndex,
		CharStart: h.CharStart, CharEnd: h.CharEnd, Excerpt: h.ChunkText,
		KeywordRank: h.KeywordRank, VectorRank: h.VectorRank,
		FinalRank: h.FinalRank, Score: h.Score,
		SourceURL: "/api/materials/" + h.MaterialID,
	}
}

func ToHitDTOs(hits []Hit) []HitDTO {
	out := make([]HitDTO, len(hits))
	for i, h := range hits {
		out[i] = ToHitDTO(h)
	}
	return out
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return errors.New("invalid_json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024))
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid_json")
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF {
		return errors.New("invalid_json")
	}
	return nil
}

// Search accepts only query/mode/limit. Unknown client fields such as
// class_id and role are ignored and cannot influence authorization.
// @Route POST /api/retrieval/search
// @Summary 第四课本班检索
// @Role class
// @Request SearchInput
// @Response 200 SearchResult
// @Error 400 invalid_query / unsupported_mode / invalid_limit
// @Error 503 retrieval_unavailable / service_unavailable
func (s *HTTPService) Search(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Query string `json:"query"`
		Mode  string `json:"mode"`
		Limit int    `json:"limit"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		auth.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" || len([]rune(input.Query)) > maxQueryRunes {
		auth.Error(w, http.StatusBadRequest, "invalid_query")
		return
	}
	if input.Mode == "" {
		input.Mode = ModeHybrid
	}
	if input.Mode != ModeKeyword && input.Mode != ModeVector && input.Mode != ModeHybrid {
		auth.Error(w, http.StatusBadRequest, "unsupported_mode")
		return
	}
	if input.Limit == 0 {
		input.Limit = defaultLimit
	}
	if input.Limit < 1 || input.Limit > maxLimit {
		auth.Error(w, http.StatusBadRequest, "invalid_limit")
		return
	}
	u := auth.Current(r)
	hits, err := s.Retriever.Retrieve(r.Context(), *u.ClassID, input.Query, input.Mode, input.Limit)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			auth.Error(w, http.StatusServiceUnavailable, "retrieval_unavailable")
		} else {
			auth.Error(w, http.StatusServiceUnavailable, "service_unavailable")
		}
		return
	}
	auth.JSON(w, http.StatusOK, map[string]any{"mode": input.Mode, "hits": ToHitDTOs(hits)})
}

type rebuildInput struct {
	Strategy           string `json:"strategy"`
	MaxLength          int    `json:"max_length"`
	OverlapPercent     int    `json:"overlap_percent"`
	Separator          string `json:"separator"`
	RemoveURLs         bool   `json:"remove_urls"`
	RemoveEmails       bool   `json:"remove_emails"`
	CollapseWhitespace bool   `json:"collapse_whitespace"`
}

func (in rebuildInput) options() knowledge.Options {
	return knowledge.Options{MaxLength: in.MaxLength, OverlapPercent: in.OverlapPercent,
		Separator: in.Separator, RemoveURLs: in.RemoveURLs, RemoveEmails: in.RemoveEmails,
		CollapseWhitespace: in.CollapseWhitespace}
}

// Rebuild is registered behind Teacher middleware. It checks same-class
// existence before parsing strategy details, keeping cross-class and missing
// IDs indistinguishable.
// @Route POST /api/materials/{id}/reindex
// @Summary 重建本班材料索引
// @Role teacher
// @Request ReindexInput
// @Response 200 ReindexResult
// @Error 400 invalid_json / invalid_strategy
// @Error 404 not_found
// @Error 503 indexing_failed / service_unavailable
func (s *HTTPService) Rebuild(w http.ResponseWriter, r *http.Request) {
	u := auth.Current(r)
	id := r.PathValue("id")
	var exists int
	err := s.DB.QueryRowContext(r.Context(),
		"SELECT 1 FROM materials WHERE id=? AND class_id=? AND status='ready'", id, *u.ClassID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		auth.Error(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		auth.Error(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var input rebuildInput
	if err := decodeJSON(w, r, &input); err != nil {
		auth.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Strategy == "" {
		input.Strategy = "auto"
	}
	// Validate before replacing the old index.
	var body string
	if err := s.DB.QueryRowContext(r.Context(), "SELECT body_text FROM knowledge_entries WHERE material_id=? AND class_id=?", id, *u.ClassID).Scan(&body); err != nil {
		auth.Error(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := knowledge.Chunks(body, input.Strategy, input.options()); err != nil {
		auth.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Indexer == nil {
		auth.Error(w, http.StatusServiceUnavailable, "indexing_unavailable")
		return
	}
	if err := s.Indexer.IndexMaterial(r.Context(), id, input.Strategy, input.options()); err != nil {
		if errors.Is(err, ErrUnavailable) {
			auth.Error(w, http.StatusServiceUnavailable, "indexing_failed")
		} else {
			auth.Error(w, http.StatusServiceUnavailable, "service_unavailable")
		}
		return
	}
	var ready, failed int
	_ = s.DB.QueryRowContext(r.Context(),
		"SELECT SUM(index_status='ready'), SUM(index_status='failed') FROM knowledge_chunks WHERE material_id=?", id).Scan(&ready, &failed)
	auth.JSON(w, http.StatusOK, map[string]any{
		"id": id, "strategy": input.Strategy, "index_status": "ready",
		"ready_chunks": ready, "failed_chunks": failed,
	})
}

func ParseLimit(raw string) (int, error) {
	if raw == "" {
		return defaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxLimit {
		return 0, errors.New("invalid_limit")
	}
	return n, nil
}
