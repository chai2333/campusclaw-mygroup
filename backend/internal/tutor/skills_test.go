package tutor

import (
	"strings"
	"testing"
)

func TestSkillParserConstraints(t *testing.T) {
	source := "# 注释\r\n名称：数学解题引导\r\n适用条件：Equation、方程；去括号|分数\r\n资料：数学\r\n工具：knowledge_search\r\n启用：是\r\n\r\n只指出下一步检查什么。"
	skill, err := ParseSkill(source)
	if err != nil {
		t.Fatal(err)
	}
	if skill.SourceText != source || skill.Name != "数学解题引导" || !skill.Enabled || len(skill.Tools) != 1 {
		t.Fatalf("lost fields: %+v", skill)
	}
	for _, bad := range []string{
		"名称：x\n适用条件：x\n\n说明",
		"名称：x\n名称：y\n适用条件：x\n启用：是\n\n说明",
		"名称：x\n适用条件：x\n工具：shell\n启用：是\n\n说明",
		"名称：x\n适用条件：x\n资料：" + strings.Repeat("文", 33) + "\n启用：是\n\n说明",
		"名称：x\n适用条件：x\n启用：是\n\n" + strings.Repeat("文", 2001),
		"名称：x\n适用条件：,;|\n启用：是\n\n说明",
		"名称：x\n适用条件：x\n启用：是\n未知：x\n\n说明",
	} {
		if _, err := ParseSkill(bad); err == nil {
			t.Errorf("accepted invalid file %q", bad[:min(len(bad), 50)])
		}
	}
}
func TestLexicalRoutingScoreTieAndDisabled(t *testing.T) {
	skills := []Skill{{ID: 9, Enabled: true, TriggerText: "Equation,去括号"}, {ID: 2, Enabled: true, TriggerText: "方程、Equation"}, {ID: 1, Enabled: false, TriggerText: "方程|equation|去括号"}}
	chosen := SelectSkill(skills, "这个 Equation 方程怎么去括号")
	if chosen == nil || chosen.ID != 2 {
		t.Fatalf("must casefold and break ties by ID: %+v", chosen)
	}
	if SelectSkill(skills, "古诗怎么默写") != nil {
		t.Fatal("zero score must not activate a skill")
	}
}
