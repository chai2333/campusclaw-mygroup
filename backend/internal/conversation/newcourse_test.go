package conversation

import (
	"campusclaw/internal/tutor"
	"context"
	"strings"
	"testing"
)

func TestZeroEvidenceStripAcrossEveryBoundary(t *testing.T) {
	source := "你好[12]，请比较[3]和[abc]，完成计算。"
	for boundary := 0; boundary <= len([]rune(source)); boundary++ {
		s := citationStripper{}
		r := []rune(source)
		got := s.Feed(string(r[:boundary])) + s.Feed(string(r[boundary:])) + s.Finish()
		if got != "你好，请比较和[abc]，完成计算。" {
			t.Fatalf("boundary %d: %s", boundary, got)
		}
	}
}
func TestAgentBindingsChangeOnlyNextTurn(t *testing.T) {
	svc, orch, retriever, gateway, classID, student, legacy := orchestrationEnv(t)
	ctx := context.Background()
	if _, err := legacy.SavePrompt(ctx, classID, 1, "旧角色"); err != nil {
		t.Fatal(err)
	}
	conv, err := svc.Create(ctx, student, classID)
	if err != nil {
		t.Fatal(err)
	}
	retriever.Hits = sampleHits(classID)
	gateway.Deltas = []string{"下一步提示[1]。"}
	manager := &tutor.Service{DB: svc.DB}
	var snapshot Turn
	outcome, err := orch.Run(ctx, svc, conv.ID, student, classID, "问题", func(turn Turn) error {
		snapshot = turn
		_, e := manager.SaveAgent(ctx, classID, turn.AgentID, tutor.AgentInput{Name: "改过的角色", Prompt: "新角色", Enabled: false, SkillIDs: []int64{}, MaterialIDs: []string{}})
		return e
	}, nil)
	if err != nil || outcome.Status != "completed" || len(outcome.Citations) != 2 {
		t.Fatalf("current turn lost snapshot: %+v %v", outcome, err)
	}
	if !strings.Contains(gateway.LastRequest.Messages[0].Content, "旧角色") || snapshot.Skill == nil {
		t.Fatal("current turn used a later config")
	}
	gateway.Deltas = []string{"没有资料也继续回答。[12]"}
	text := ""
	outcome, err = orch.Run(ctx, svc, conv.ID, student, classID, "问题", nil, func(d string) error { text += d; return nil })
	if err != nil || outcome.Status != "completed" || len(outcome.Citations) != 0 || strings.Contains(text, "[12]") {
		t.Fatalf("next turn/no-hit state: %+v %v %s", outcome, err, text)
	}
	if !strings.Contains(gateway.LastRequest.Messages[0].Content, "新角色") || strings.Contains(gateway.LastRequest.Messages[0].Content, "【当前技能】") {
		t.Fatal("next turn did not reread binding config")
	}
	if _, err := svc.Create(ctx, student, classID, conv.AgentID); err == nil {
		t.Fatal("disabled agent accepted a new conversation")
	}
}
