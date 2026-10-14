package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrUnavailable reports that a retrieval dependency (embedding gateway or
// vector store) is unavailable or not configured. Keyword retrieval never
// returns it.
var ErrUnavailable = errors.New("retrieval_unavailable")

const (
	ModeKeyword = "keyword"
	ModeVector  = "vector"
	ModeHybrid  = "hybrid"

	// MinVectorScore is the absolute cosine-similarity filter for the vector
	// path, applied before any fusion.
	MinVectorScore = 0.35

	// RRFK is the reciprocal rank fusion constant.
	RRFK = 60.0
)

// Hit is one traceable retrieval result. Text always comes from MySQL.
type Hit struct {
	ChunkID       int64
	MaterialID    string
	MaterialTitle string
	ClassID       int64
	ChunkIndex    int
	ChunkText     string
	CharStart     int
	CharEnd       int
	Score         float64
	KeywordRank   int // 1-based rank in the keyword path; 0 = absent
	VectorRank    int // 1-based rank in the vector path; 0 = absent
	FinalRank     int
}

// Retriever serves class-scoped retrieval. The class ID always comes from the
// server-side session; clients cannot supply it.
type Retriever interface {
	Retrieve(ctx context.Context, classID int64, query, mode string, limit int) ([]Hit, error)
}

// Service implements Retriever against MySQL and the optional vector
// pipeline. When the vector pipeline is disabled, vector mode fails with
// ErrUnavailable; hybrid also fails rather than hiding an unavailable path.
type Service struct {
	DB        *sql.DB
	Embedder  Embedder
	Vectors   VectorIndex
	materials []string
}

// VectorEnabled reports whether the vector path can serve requests.
func (s *Service) VectorEnabled() bool {
	return s != nil && s.Embedder != nil && s.Vectors != nil
}

// Retrieve returns at most limit hits for the given class. An empty mode
// means hybrid. A blank query is rejected without touching any dependency.
func (s *Service) Retrieve(ctx context.Context, classID int64, query, mode string, limit int) ([]Hit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("blank_query")
	}
	if limit < 1 {
		limit = 4
	}
	switch mode {
	case "", ModeHybrid:
		return s.hybrid(ctx, classID, q, limit)
	case ModeKeyword:
		return s.keyword(ctx, classID, q, limit)
	case ModeVector:
		return s.vector(ctx, classID, q, limit)
	default:
		return nil, errors.New("unsupported_mode")
	}
}

// BoundRetriever limits retrieval at each source rather than filtering a capped
// full-class result after the fact.
type BoundRetriever interface {
	RetrieveBound(context.Context, int64, string, string, int, []string) ([]Hit, error)
}

func (s *Service) RetrieveBound(ctx context.Context, classID int64, query, mode string, limit int, materials []string) ([]Hit, error) {
	if len(materials) == 0 {
		return []Hit{}, nil
	}
	clone := *s
	clone.materials = append([]string{}, materials...)
	return clone.Retrieve(ctx, classID, query, mode, limit)
}
func (s *Service) scope(alias string) (string, []any) {
	if s.materials == nil {
		return "", nil
	}
	args := []any{}
	for _, m := range s.materials {
		args = append(args, m)
	}
	return " AND " + alias + ".material_id IN (" + strings.TrimRight(strings.Repeat("?,", len(args)), ",") + ")", args
}

const hitColumns = `c.id, c.material_id, m.title, c.class_id, c.chunk_index, c.chunk_text, c.char_start, c.char_end`

func scanHits(rows *sql.Rows) ([]Hit, error) {
	hits := []Hit{}
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ChunkID, &h.MaterialID, &h.MaterialTitle, &h.ClassID, &h.ChunkIndex, &h.ChunkText, &h.CharStart, &h.CharEnd); err != nil {
			rows.Close()
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// keyword queries the class-scoped ngram FULLTEXT index and orders by
// relevance. It never touches the embedding gateway or vector store.
func (s *Service) keyword(ctx context.Context, classID int64, q string, limit int) ([]Hit, error) {
	scope, args := s.scope("c")
	params := []any{q, classID, q}
	params = append(params, args...)
	params = append(params, limit)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+hitColumns+`, MATCH(c.chunk_text) AGAINST(? IN NATURAL LANGUAGE MODE) AS score
 FROM knowledge_chunks c JOIN materials m ON m.id=c.material_id AND m.class_id=c.class_id
 WHERE c.class_id=? AND c.index_status='ready' AND m.status='ready' AND MATCH(c.chunk_text) AGAINST(? IN NATURAL LANGUAGE MODE)`+scope+`
 ORDER BY score DESC,c.id ASC LIMIT ?`, params...)
	if err != nil {
		return nil, err
	}
	hits := []Hit{}
	for rows.Next() {
		var h Hit
		var score float64
		if err := rows.Scan(&h.ChunkID, &h.MaterialID, &h.MaterialTitle, &h.ClassID, &h.ChunkIndex, &h.ChunkText, &h.CharStart, &h.CharEnd, &score); err != nil {
			rows.Close()
			return nil, err
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range hits {
		hits[i].KeywordRank = i + 1
	}
	return hits, nil
}

// vector embeds the query, searches the vector store filtered to the class,
// drops candidates below the absolute threshold and re-reads the survivors
// from MySQL with the same class and ready constraints.
func (s *Service) vector(ctx context.Context, classID int64, q string, limit int) ([]Hit, error) {
	if !s.VectorEnabled() {
		return nil, ErrUnavailable
	}
	vectors, err := s.Embedder.Embed(ctx, []string{q})
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(vectors) != 1 {
		return nil, ErrUnavailable
	}
	var candidates []VectorHit
	if s.materials != nil {
		bound, ok := s.Vectors.(interface {
			SearchBound(context.Context, int64, []float32, int, []string) ([]VectorHit, error)
		})
		if !ok {
			return nil, ErrUnavailable
		}
		candidates, err = bound.SearchBound(ctx, classID, vectors[0], limit*4, s.materials)
	} else {
		candidates, err = s.Vectors.Search(ctx, classID, vectors[0], limit*4)
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	filtered := []VectorHit{}
	for _, c := range candidates {
		if float64(c.Score) >= MinVectorScore {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return []Hit{}, nil
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	hits, err := s.fetchByIDs(ctx, classID, filtered, true)
	if err != nil {
		return nil, err
	}
	for i := range hits {
		hits[i].VectorRank = i + 1
	}
	return hits, nil
}

// fetchByIDs re-reads chunks from MySQL, keeping only ready chunks of the
// given class, ordered by score when descending is set.
func (s *Service) fetchByIDs(ctx context.Context, classID int64, candidates []VectorHit, byScore bool) ([]Hit, error) {
	ids := make([]int64, len(candidates))
	scores := map[int64]float64{}
	for i, c := range candidates {
		ids[i] = c.ChunkID
		scores[c.ChunkID] = float64(c.Score)
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, classID)
	scope, scopeArgs := s.scope("c")
	args = append(args, scopeArgs...)
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+hitColumns+` FROM knowledge_chunks c JOIN materials m ON m.id=c.material_id AND m.class_id=c.class_id
		 WHERE c.id IN (`+placeholders+`) AND c.class_id=? AND c.index_status='ready' AND m.status='ready'`+scope, args...)
	if err != nil {
		return nil, err
	}
	hits, err := scanHits(rows)
	if err != nil {
		return nil, err
	}
	for i := range hits {
		hits[i].Score = scores[hits[i].ChunkID]
	}
	if byScore {
		// Keep vector order (highest similarity first).
		order := map[int64]int{}
		for i, c := range candidates {
			order[c.ChunkID] = i
		}
		for i := 1; i < len(hits); i++ {
			for j := i; j > 0 && order[hits[j].ChunkID] < order[hits[j-1].ChunkID]; j-- {
				hits[j], hits[j-1] = hits[j-1], hits[j]
			}
		}
	}
	return hits, nil
}

// hybrid runs both paths, applies each path's absolute filter first, then
// fuses ranks with reciprocal rank fusion (k=60). A chunk absent from one
// path receives no contribution from that path; stable chunk IDs break ties.
func (s *Service) hybrid(ctx context.Context, classID int64, q string, limit int) ([]Hit, error) {
	if !s.VectorEnabled() {
		return nil, ErrUnavailable
	}
	type pathResult struct {
		hits []Hit
		err  error
	}
	keywordCh := make(chan pathResult, 1)
	vectorCh := make(chan pathResult, 1)
	go func() {
		hits, err := s.keyword(ctx, classID, q, limit*4)
		keywordCh <- pathResult{hits: hits, err: err}
	}()
	go func() {
		hits, err := s.vector(ctx, classID, q, limit*4)
		vectorCh <- pathResult{hits: hits, err: err}
	}()
	keywordResult, vectorResult := <-keywordCh, <-vectorCh
	if keywordResult.err != nil {
		return nil, keywordResult.err
	}
	if vectorResult.err != nil {
		return nil, vectorResult.err
	}
	keywordHits, vectorHits := keywordResult.hits, vectorResult.hits
	byID := map[int64]*Hit{}
	for i := range keywordHits {
		h := keywordHits[i]
		h.Score = 0
		byID[h.ChunkID] = &h
	}
	for i := range vectorHits {
		if _, ok := byID[vectorHits[i].ChunkID]; !ok {
			h := vectorHits[i]
			h.Score = 0
			byID[h.ChunkID] = &h
		}
	}
	for rank, h := range keywordHits {
		byID[h.ChunkID].Score += 1 / float64(RRFK+rank+1)
	}
	for rank, h := range vectorHits {
		byID[h.ChunkID].Score += 1 / float64(RRFK+rank+1)
	}
	fused := make([]Hit, 0, len(byID))
	for _, h := range byID {
		fused = append(fused, *h)
	}
	for i := 1; i < len(fused); i++ {
		for j := i; j > 0; j-- {
			a, b := fused[j], fused[j-1]
			if a.Score > b.Score || (a.Score == b.Score && a.ChunkID < b.ChunkID) {
				fused[j], fused[j-1] = fused[j-1], fused[j]
			} else {
				break
			}
		}
	}
	if len(fused) > limit {
		fused = fused[:limit]
	}
	for i := range fused {
		fused[i].FinalRank = i + 1
	}
	return fused, nil
}
