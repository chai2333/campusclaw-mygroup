package dialogue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// GroundedInput is the fourth-lesson dialogue-module boundary. The caller
// supplies only the current question and the ordered, class-scoped evidence;
// no vectors, database handles, or cross-class material are available here.
type GroundedInput struct {
	Question string             `json:"question"`
	Evidence []GroundedEvidence `json:"evidence"`
}

type GroundedEvidence struct {
	Ordinal    int    `json:"ordinal"`
	Title      string `json:"title"`
	ChunkIndex int    `json:"chunk_index"`
	Text       string `json:"text"`
}

// ExtractiveGateway generates a short, cited answer without an external
// model. It copies the most question-relevant statement from the supplied
// evidence, so every emitted claim is directly verifiable in a cited chunk.
type ExtractiveGateway struct{}

func (ExtractiveGateway) Stream(ctx context.Context, req Request, onDelta func(string) error) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(req.Messages) == 0 {
		return "", ErrUnavailable
	}
	var input GroundedInput
	if err := json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &input); err != nil || strings.TrimSpace(input.Question) == "" || len(input.Evidence) == 0 {
		return "", ErrUnavailable
	}

	type candidate struct {
		text    string
		ordinal int
		score   int
	}
	best := candidate{score: -1}
	for _, evidence := range input.Evidence {
		if evidence.Ordinal < 1 {
			continue
		}
		for _, statement := range evidenceStatements(evidence.Text) {
			score := overlapScore(input.Question, statement)
			if score > best.score {
				best = candidate{text: statement, ordinal: evidence.Ordinal, score: score}
			}
		}
	}
	if best.text == "" {
		return "", ErrUnavailable
	}
	answer := fmt.Sprintf("根据资料，%s[%d]", ensureSentenceEnd(best.text), best.ordinal)
	if onDelta != nil {
		if err := onDelta(answer); err != nil {
			return "", err
		}
	}
	return answer, nil
}

func evidenceStatements(text string) []string {
	statements := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#*-+>0123456789.、 \t"))
		if line == "" {
			continue
		}
		for _, part := range strings.FieldsFunc(line, func(r rune) bool {
			return r == '。' || r == '！' || r == '？' || r == ';' || r == '；'
		}) {
			part = strings.TrimSpace(part)
			if part != "" {
				statements = append(statements, part)
			}
		}
	}
	return statements
}

func overlapScore(question, statement string) int {
	q := normalizedRunes(question)
	s := normalizedRunes(statement)
	if len(q) == 0 || len(s) == 0 {
		return 0
	}
	qgrams := map[string]struct{}{}
	for i := 0; i+2 <= len(q); i++ {
		qgrams[string(q[i:i+2])] = struct{}{}
	}
	score := 0
	for i := 0; i+2 <= len(s); i++ {
		if _, ok := qgrams[string(s[i:i+2])]; ok {
			score++
		}
	}
	return score
}

func normalizedRunes(value string) []rune {
	value = strings.ToLower(value)
	out := make([]rune, 0, len([]rune(value)))
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, r)
		}
	}
	return out
}

func ensureSentenceEnd(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	last := []rune(value)[len([]rune(value))-1]
	if strings.ContainsRune("。！？.!?", last) {
		return value
	}
	return value + "。"
}

var _ Gateway = ExtractiveGateway{}
