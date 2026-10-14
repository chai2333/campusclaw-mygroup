package dialogue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestExtractiveGatewaySelectsRelevantStatement(t *testing.T) {
	input := GroundedInput{Question: "第二讲介绍了什么？", Evidence: []GroundedEvidence{{
		Ordinal: 1, Title: "A班课程导览", ChunkIndex: 1,
		Text: "## 课程安排\n\n- 第一讲：入门\n- 第二讲：进阶",
	}}}
	body, _ := json.Marshal(input)
	var delta string
	got, err := (ExtractiveGateway{}).Stream(context.Background(), Request{Messages: []Message{{Role: "user", Content: string(body)}}}, func(value string) error {
		delta += value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "根据资料，第二讲：进阶。[1]" || delta != got {
		t.Fatalf("unexpected local answer: %q / %q", got, delta)
	}
}

func TestExtractiveGatewayRejectsInvalidInputAndCallbackFailure(t *testing.T) {
	gateway := ExtractiveGateway{}
	if _, err := gateway.Stream(context.Background(), Request{}, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty input must fail safely: %v", err)
	}
	input := GroundedInput{Question: "课程", Evidence: []GroundedEvidence{{Ordinal: 1, Text: "课程安排"}}}
	body, _ := json.Marshal(input)
	want := errors.New("stop")
	_, err := gateway.Stream(context.Background(), Request{Messages: []Message{{Role: "user", Content: string(body)}}}, func(string) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("callback error not propagated: %v", err)
	}
}
