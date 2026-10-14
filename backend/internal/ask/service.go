// Package ask implements the fourth-lesson, non-streaming grounded answer
// endpoint. Retrieval is a separate dependency; this package never invents a
// third search path.
package ask

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"campusclaw/internal/auth"
	"campusclaw/internal/dialogue"
	"campusclaw/internal/retrieval"
)

const noEvidenceAnswer = "资料中未找到相关内容"
const unsupportedAnswer = "无法根据检索到的资料可靠回答"

var ErrModelNotConfigured = errors.New("dialogue model not configured")

var citationPattern = regexp.MustCompile(`\[(\d+)\]`)

type Service struct {
	Retriever retrieval.Retriever
	Gateway   dialogue.Gateway
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

type Citation struct {
	Ordinal       int    `json:"ordinal"`
	MaterialID    string `json:"material_id"`
	MaterialTitle string `json:"material_title"`
	ChunkID       int64  `json:"chunk_id"`
	ChunkIndex    int    `json:"chunk_index"`
	CharStart     int    `json:"char_start"`
	CharEnd       int    `json:"char_end"`
	Excerpt       string `json:"excerpt"`
	SourceURL     string `json:"source_url"`
}

type Response struct {
	Answer    string             `json:"answer"`
	Hits      []retrieval.HitDTO `json:"hits"`
	Citations []Citation         `json:"citations"`
}

// @Route POST /api/ask
// @Summary 第四课单次知识库问答
// @Role class
// @Request AskInput
// @Response 200 AskResult
// @Error 400 invalid_json / invalid_question
// @Error 503 answer_unavailable / service_unavailable
func (s *Service) Ask(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Question string `json:"question"`
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		auth.Error(w, http.StatusBadRequest, "invalid_json")
		return
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024))
	if err := dec.Decode(&input); err != nil {
		auth.Error(w, http.StatusBadRequest, "invalid_json")
		return
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF {
		auth.Error(w, http.StatusBadRequest, "invalid_json")
		return
	}
	question := strings.TrimSpace(input.Question)
	if question == "" || len([]rune(question)) > 2000 {
		auth.Error(w, http.StatusBadRequest, "invalid_question")
		return
	}
	u := auth.Current(r)
	resp, err := s.Answer(r.Context(), *u.ClassID, question)
	if err != nil {
		if errors.Is(err, ErrModelNotConfigured) {
			auth.Error(w, http.StatusServiceUnavailable, "model_not_configured")
			return
		}
		auth.Error(w, http.StatusServiceUnavailable, "answer_unavailable")
		return
	}
	auth.JSON(w, http.StatusOK, resp)
}

func (s *Service) Answer(ctx context.Context, classID int64, question string) (Response, error) {
	hits, err := s.Retriever.Retrieve(ctx, classID, question, retrieval.ModeHybrid, 4)
	if err != nil {
		return Response{}, err
	}
	if len(hits) == 0 {
		return Response{Answer: noEvidenceAnswer, Hits: []retrieval.HitDTO{}, Citations: []Citation{}}, nil
	}
	if len(hits) > 4 {
		hits = hits[:4]
	}
	if s.Gateway == nil {
		return Response{}, ErrModelNotConfigured
	}
	evidenceInput := dialogue.GroundedInput{Question: question, Evidence: make([]dialogue.GroundedEvidence, 0, len(hits))}
	for i, h := range hits {
		evidenceInput.Evidence = append(evidenceInput.Evidence, dialogue.GroundedEvidence{
			Ordinal: i + 1, Title: h.MaterialTitle, ChunkIndex: h.ChunkIndex, Text: h.ChunkText,
		})
	}
	payload, err := json.Marshal(evidenceInput)
	if err != nil {
		return Response{}, err
	}
	req := dialogue.Request{Model: s.Model, MaxTokens: s.MaxTokens, Messages: []dialogue.Message{
		{Role: "system", Content: "你是班级知识库问答助手。只能根据提供的材料回答；回答应简短，并在相关句末使用 [1]、[2] 形式引用。不得使用材料外知识。"},
		{Role: "user", Content: string(payload)},
	}}
	genCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		genCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	answer, err := s.Gateway.Stream(genCtx, req, nil)
	if err != nil {
		return Response{}, err
	}
	answer = strings.TrimSpace(answer)
	ordinals, ok := citationOrdinals(answer, len(hits))
	if !ok {
		return Response{}, errors.New("invalid_citations")
	}
	if !answerSupportedByCitations(answer, hits, ordinals) {
		return Response{Answer: unsupportedAnswer, Hits: retrieval.ToHitDTOs(hits), Citations: []Citation{}}, nil
	}
	citations := make([]Citation, 0, len(ordinals))
	for _, ordinal := range ordinals {
		h := hits[ordinal-1]
		citations = append(citations, Citation{Ordinal: ordinal, MaterialID: h.MaterialID,
			MaterialTitle: h.MaterialTitle, ChunkID: h.ChunkID, ChunkIndex: h.ChunkIndex,
			CharStart: h.CharStart, CharEnd: h.CharEnd, Excerpt: h.ChunkText,
			SourceURL: "/api/materials/" + h.MaterialID})
	}
	return Response{Answer: answer, Hits: retrieval.ToHitDTOs(hits), Citations: citations}, nil
}

// answerSupportedByCitations is a deliberately conservative lexical guard.
// It is not a semantic judge; its purpose is to stop a provider from attaching
// a valid-looking citation number to an answer that shares no meaningful text
// with the cited MySQL chunks.
func answerSupportedByCitations(answer string, hits []retrieval.Hit, ordinals []int) bool {
	clean := citationPattern.ReplaceAllString(answer, "")
	answer2, answer3 := ngrams(clean, 2), ngrams(clean, 3)
	for _, ordinal := range ordinals {
		text := hits[ordinal-1].ChunkText
		if intersects(answer3, ngrams(text, 3), 1) || intersects(answer2, ngrams(text, 2), 2) || sharedLongWord(clean, text) {
			return true
		}
	}
	return false
}

func ngrams(text string, size int) map[string]struct{} {
	normalized := make([]rune, 0, len([]rune(text)))
	for _, r := range []rune(strings.ToLower(text)) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= '\u4e00' && r <= '\u9fff') {
			normalized = append(normalized, r)
		}
	}
	out := map[string]struct{}{}
	for i := 0; i+size <= len(normalized); i++ {
		out[string(normalized[i:i+size])] = struct{}{}
	}
	return out
}

func intersects(left, right map[string]struct{}, minimum int) bool {
	count := 0
	for value := range left {
		if _, ok := right[value]; ok {
			count++
			if count >= minimum {
				return true
			}
		}
	}
	return false
}

func sharedLongWord(left, right string) bool {
	right = strings.ToLower(right)
	for _, word := range strings.FieldsFunc(strings.ToLower(left), func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'z'))
	}) {
		if len(word) >= 4 && strings.Contains(right, word) {
			return true
		}
	}
	return false
}

func citationOrdinals(answer string, count int) ([]int, bool) {
	matches := citationPattern.FindAllStringSubmatch(answer, -1)
	if strings.TrimSpace(answer) == "" || len(matches) == 0 {
		return nil, false
	}
	seen := map[int]bool{}
	ordinals := []int{}
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > count {
			return nil, false
		}
		if !seen[n] {
			seen[n] = true
			ordinals = append(ordinals, n)
		}
	}
	sort.Ints(ordinals)
	return ordinals, true
}
