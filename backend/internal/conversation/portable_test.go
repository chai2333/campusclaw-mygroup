package conversation

import (
	"context"
	"strings"
	"testing"

	"campusclaw/internal/tutor"
)

func TestPortableSkillSharedAcrossAgents(t *testing.T) {
	svc, orch, _, gateway, classID, student, _ := orchestrationEnv(t)
	ctx := context.Background()
	manager := &tutor.Service{DB: svc.DB}
	source := "---\nname: english-writing-review\ndescription: Review English essays\nmetadata:\n  campusclaw-display-name: 英语作文批改\n  campusclaw-triggers: 作文,essay\n  campusclaw-enabled: \"true\"\n---\n\n解释语法错误并保留作者原意，区分必要修正和可选风格建议。"
	skill, err := tutor.ParseSkill(source)
	if err != nil {
		t.Fatal(err)
	}
	skill, err = manager.SaveSkill(ctx, classID, 0, skill)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"英语写作助手", "综合学习助手"} {
		agent, err := manager.SaveAgent(ctx, classID, 0, tutor.AgentInput{Name: name, Prompt: "帮助学生学习", Enabled: true, SkillIDs: []int64{skill.ID}, MaterialIDs: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		conv, err := svc.Create(ctx, student, classID, agent.ID)
		if err != nil {
			t.Fatal(err)
		}
		gateway.Deltas = []string{"go应改为goes，因为主语是第三人称单数。"}
		var selected *tutor.Skill
		outcome, err := orch.Run(ctx, svc, conv.ID, student, classID, "请批改英语作文：She go to school.", func(turn Turn) error { selected = turn.Skill; return nil }, nil)
		if err != nil || outcome.Status != "completed" || selected == nil || selected.ID != skill.ID {
			t.Fatalf("selection: %+v %v", outcome, err)
		}
		if !strings.Contains(gateway.LastRequest.Messages[0].Content, skill.Description) {
			t.Fatal("imported skill missing from model context")
		}
	}
	var otherClass int64
	if err = svc.DB.QueryRow("SELECT id FROM classes WHERE slug='b'").Scan(&otherClass); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.SaveAgent(ctx, otherClass, 0, tutor.AgentInput{Name: "外班助手", Enabled: true, SkillIDs: []int64{skill.ID}}); err != tutor.ErrNotFound {
		t.Fatalf("cross-class binding: %v", err)
	}
}
