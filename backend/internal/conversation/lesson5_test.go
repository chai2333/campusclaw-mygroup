package conversation

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExplicitAnswerGuard(t *testing.T) {
	for _, text := range []string{"最终答案是 42。", "最终答案：42", "答案为 12 米", "FINAL_ANSWER: 42", "The final answer is 42"} {
		if !explicitFinalAnswer.MatchString(text) {
			t.Errorf("missed explicit answer %q", text)
		}
	}
	for _, text := range []string{"不要直接给出最终答案。", "请你算出最终答案，再检查单位。", "先写公式 s=v*t，再自行完成计算。"} {
		if explicitFinalAnswer.MatchString(text) {
			t.Errorf("rejected guidance %q", text)
		}
	}
}

func TestEvidenceIncludesSourcePosition(t *testing.T) {
	messages := assembleMessages("教师角色", true, nil, buildEvidence(sampleHits(1)), "问题")
	if !strings.Contains(messages[len(messages)-1].Content, "位置：切片 1，字符范围 [8, 15)") {
		t.Fatal("model evidence misses source position")
	}
}

func TestQuestionRejectsTrailingJSON(t *testing.T) {
	for _, body := range []string{`{"text":"问题"} {"text":"第二个对象"}`, `{"text":"问题"} trailing`, `{"text":"问题","class_id":2}`} {
		r := httptest.NewRequest("POST", "/api/conversations/c/messages", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		var input questionRequest
		if decodeQuestion(w, r, &input) || w.Code != 400 {
			t.Fatalf("accepted invalid JSON: %s", body)
		}
	}
}
