package ask

import (
	"context"
	"errors"
	"strings"
	"testing"

	"campusclaw/internal/dialogue"
	"campusclaw/internal/dialogue/dialoguetest"
	"campusclaw/internal/retrieval"
	"campusclaw/internal/retrieval/retrievaltest"
)

func TestNoEvidenceShortCircuitsGateway(t *testing.T) {
	r := &retrievaltest.FakeRetriever{}
	g := &dialoguetest.FakeGateway{Deltas: []string{"不应调用"}}
	s := &Service{Retriever: r, Gateway: g, Model: "m"}
	got, err := s.Answer(context.Background(), 7, "资料中没有的问题")
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer != "资料中未找到相关内容" || len(got.Hits) != 0 || len(got.Citations) != 0 {
		t.Fatalf("unexpected zero-hit response: %+v", got)
	}
	if g.Calls != 0 {
		t.Fatal("dialogue gateway must not be called for zero hits")
	}
	if r.LastClassID != 7 || r.LastMode != retrieval.ModeHybrid || r.LastLimit != 4 {
		t.Fatalf("ask must use class-scoped hybrid top 4: %+v", r)
	}
}

func TestGroundedAnswerSendsOnlyEvidenceAndQuestion(t *testing.T) {
	r := &retrievaltest.FakeRetriever{Hits: []retrieval.Hit{
		{ChunkID: 11, MaterialID: "a", MaterialTitle: "A班讲义", ClassID: 3, ChunkIndex: 2, ChunkText: "第一讲介绍校园安全。", CharStart: 10, CharEnd: 21},
		{ChunkID: 12, MaterialID: "b", MaterialTitle: "课程安排", ClassID: 3, ChunkIndex: 0, ChunkText: "第一讲为入门。", CharStart: 0, CharEnd: 8},
	}}
	g := &dialoguetest.FakeGateway{Full: "第一讲介绍校园安全，并作为课程入门。[1][2]"}
	s := &Service{Retriever: r, Gateway: g, Model: "m", MaxTokens: 300}
	got, err := s.Answer(context.Background(), 3, "第一讲是什么？")
	if err != nil {
		t.Fatal(err)
	}
	if g.Calls != 1 || len(g.LastRequest.Messages) != 2 {
		t.Fatalf("unexpected gateway call: %+v", g.LastRequest)
	}
	joined := g.LastRequest.Messages[0].Content + g.LastRequest.Messages[1].Content
	for _, want := range []string{"A班讲义", `"chunk_index":2`, "第一讲介绍校园安全。", "第一讲是什么？"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("gateway input missing %q: %s", want, joined)
		}
	}
	for _, forbidden := range []string{"vector", "class_id", "ClassID"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("gateway input leaked %q: %s", forbidden, joined)
		}
	}
	if len(got.Citations) != 2 || got.Citations[0].Ordinal != 1 || got.Citations[0].SourceURL != "/api/materials/a" {
		t.Fatalf("unexpected citations: %+v", got.Citations)
	}
}

func TestLocalExtractiveAnswerAndCitation(t *testing.T) {
	r := &retrievaltest.FakeRetriever{Hits: []retrieval.Hit{{
		ChunkID: 11, MaterialID: "a", MaterialTitle: "A班课程导览", ClassID: 3,
		ChunkIndex: 1, ChunkText: "## 课程安排\n\n- 第一讲：入门\n- 第二讲：进阶", CharStart: 38, CharEnd: 65,
	}}}
	s := &Service{Retriever: r, Gateway: dialogue.ExtractiveGateway{}}
	got, err := s.Answer(context.Background(), 3, "第一讲介绍了什么？")
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer != "根据资料，第一讲：入门。[1]" {
		t.Fatalf("unexpected extractive answer: %q", got.Answer)
	}
	if len(got.Citations) != 1 || got.Citations[0].Ordinal != 1 || got.Citations[0].Excerpt != r.Hits[0].ChunkText {
		t.Fatalf("unexpected citations: %+v", got.Citations)
	}
}

func TestMissingModelAndUnsupportedAnswerNeverFabricate(t *testing.T) {
	r := &retrievaltest.FakeRetriever{Hits: []retrieval.Hit{{
		ChunkID: 11, MaterialID: "a", MaterialTitle: "A班课程导览", ClassID: 3,
		ChunkIndex: 1, ChunkText: "课程安排：第一讲为入门，第二讲为进阶。",
	}}}
	withoutModel := &Service{Retriever: r}
	if _, err := withoutModel.Answer(context.Background(), 3, "第一讲介绍什么？"); !errors.Is(err, ErrModelNotConfigured) {
		t.Fatalf("missing model must be explicit: %v", err)
	}

	g := &dialoguetest.FakeGateway{Full: "匀速直线运动是速度保持不变的运动，其加速度为零。[1]"}
	withBadModel := &Service{Retriever: r, Gateway: g, Model: "bad-model"}
	got, err := withBadModel.Answer(context.Background(), 3, "第一讲介绍什么？")
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer != unsupportedAnswer || len(got.Citations) != 0 {
		t.Fatalf("unsupported provider answer escaped: %+v", got)
	}
}

func TestInvalidModelCitationIsRejected(t *testing.T) {
	r := &retrievaltest.FakeRetriever{Hits: []retrieval.Hit{{ChunkID: 1, MaterialID: "a", MaterialTitle: "A", ChunkText: "正文"}}}
	for _, answer := range []string{"无引用回答", "越界引用。[2]", ""} {
		g := &dialoguetest.FakeGateway{Full: answer}
		s := &Service{Retriever: r, Gateway: g}
		if _, err := s.Answer(context.Background(), 1, "问题"); err == nil {
			t.Fatalf("answer %q must be rejected", answer)
		}
	}
}

func TestRetrievalAndDialogueFailuresPropagate(t *testing.T) {
	r := &retrievaltest.FakeRetriever{Err: retrieval.ErrUnavailable}
	s := &Service{Retriever: r, Gateway: &dialoguetest.FakeGateway{}}
	if _, err := s.Answer(context.Background(), 1, "问题"); !errors.Is(err, retrieval.ErrUnavailable) {
		t.Fatalf("unexpected retrieval error: %v", err)
	}
	r.Err = nil
	r.Hits = []retrieval.Hit{{ChunkID: 1, MaterialID: "a", MaterialTitle: "A", ChunkText: "正文"}}
	s.Gateway = &dialoguetest.FakeGateway{Err: dialogue.ErrTimeout}
	if _, err := s.Answer(context.Background(), 1, "问题"); !errors.Is(err, dialogue.ErrTimeout) {
		t.Fatalf("unexpected dialogue error: %v", err)
	}
}
