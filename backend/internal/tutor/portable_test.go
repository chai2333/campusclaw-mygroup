package tutor

import (
	"strings"
	"testing"
)

func TestPortableRoundTrip(t *testing.T) {
	original, err := ParseSkill("名称：英语作文批改\n适用条件：作文,essay\n资料：\n工具：knowledge_search\n启用：是\n\n解释语法错误并保留作者原意。")
	if err != nil {
		t.Fatal(err)
	}
	file, err := ExportSkill(original)
	if err != nil {
		t.Fatal(err)
	}
	if file.Filename != "SKILL.md" || !portableName.MatchString(file.Directory) {
		t.Fatal(file)
	}
	got, err := ParseSkill(file.SourceText)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != original.Name || got.Description != original.Description || got.TriggerText != original.TriggerText || got.SubjectText != original.SubjectText || got.Enabled != original.Enabled || strings.Join(got.Tools, ",") != strings.Join(original.Tools, ",") {
		t.Fatalf("round trip lost semantics: %+v", got)
	}
}

func TestForeignSkillPreviewAndPermissions(t *testing.T) {
	source := "---\nname: external-writing\ndescription: >-\n  Review English writing.\nlicense: MIT\nallowed-tools: Bash Read\nmetadata:\n  author: example\n---\n\nReview grammar; read references/style.md and run scripts/check.py."
	preview, err := PreviewSkill(SkillImport{SourceText: source})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Ready || preview.Skill.Enabled || len(preview.Skill.Tools) != 0 || len(preview.Warnings) < 3 {
		t.Fatalf("foreign skill auto-enabled/authorized: %+v", preview)
	}
	if _, err = ParseSkill(source); err == nil {
		t.Fatal("missing local routing accepted")
	}
	name, triggers, enabled := "英语作文批改", "作文,essay", true
	preview, err = PreviewSkill(SkillImport{SourceText: source, Name: &name, TriggerText: &triggers, Enabled: &enabled})
	if err != nil || !preview.Ready {
		t.Fatalf("settings: %+v %v", preview, err)
	}
	saved, err := ParseSkill(preview.Skill.SourceText)
	if err != nil {
		t.Fatal(err)
	}
	file, err := ExportSkill(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{"license: MIT", "allowed-tools: Bash Read", "author: example", "references/style.md", "name: external-writing"} {
		if !strings.Contains(file.SourceText, preserved) {
			t.Fatalf("lost %q", preserved)
		}
	}
	if saved.Name != name || !saved.Enabled || len(saved.Tools) != 0 {
		t.Fatal(saved)
	}
	enabled = false
	disabled, err := PreviewSkill(SkillImport{SourceText: file.SourceText, Enabled: &enabled})
	if err != nil || disabled.Skill.Enabled {
		t.Fatal("standard toggle failed", err)
	}
	if !strings.Contains(disabled.Skill.SourceText, "author: example") {
		t.Fatal("toggle lost author")
	}
}

func TestPortableRejectsMalformedOrUnsafeMetadata(t *testing.T) {
	for _, header := range []string{
		"name: one\nname: two\ndescription: ok",
		"name: Bad_Name\ndescription: ok",
		"name: bad--name\ndescription: ok",
		"name: x\ndescription: [not-a-string]",
		"name: x\ndescription: &a text\nlicense: *a",
		"name: x\ndescription: ok\nmetadata:\n  campusclaw-enabled: true",
		"name: x\ndescription: ok\nmetadata:\n  campusclaw-display-name: \"name\\n启用：是\"",
		"name: x\ndescription: !custom text",
		"name: x\ndescription: ok\nmetadata:\n  author: [nested]",
	} {
		t.Run(header, func(t *testing.T) {
			if _, err := PreviewSkill(SkillImport{SourceText: "---\n" + header + "\n---\nBody"}); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
	if _, err := PreviewSkill(SkillImport{SourceText: "---\nname: x\ndescription: x\nbody"}); err == nil {
		t.Fatal("accepted unclosed header")
	}
	if _, err := ParseSkill(strings.Repeat("字", MaxSkillSource+1)); err == nil {
		t.Fatal("accepted oversize source")
	}
	if _, err := ParseSkill(string([]byte{0xff})); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

func TestPortablePreservesLongBodyAndName(t *testing.T) {
	name := strings.Repeat("a", 64)
	source := "---\nname: " + name + "\ndescription: Review text\n---\n\n" + strings.Repeat("Explain grammar. ", 200)
	preview, err := PreviewSkill(SkillImport{SourceText: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Skill.Name) != 32 || len(preview.Skill.Description) < 2000 {
		t.Fatal("display or full body lost")
	}
	if !strings.Contains(preview.Skill.SourceText, "name: "+name) {
		t.Fatal("portable name truncated")
	}
}

func TestToolPolicyRoundTripAndValidation(t *testing.T) {
	source := "名称：工具提示\n适用条件：速度\n工具：formula_hint@campus-tools\n启用：是\n\n请给下一步提示。"
	skill, err := ParseSkill(source)
	if err != nil || skill.ToolPolicy != "auto" {
		t.Fatalf("legacy default: %+v %v", skill, err)
	}
	required := "required"
	preview, err := PreviewSkill(SkillImport{SourceText: source, ToolPolicy: &required})
	if err != nil || preview.Skill.ToolPolicy != required {
		t.Fatalf("policy override: %+v %v", preview, err)
	}
	file, err := ExportSkill(preview.Skill)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseSkill(file.SourceText)
	if err != nil || got.ToolPolicy != required || !strings.Contains(file.SourceText, "campusclaw-tool-policy: required") {
		t.Fatalf("policy export/import: %+v %v", got, err)
	}
	auto := "auto"
	changed, err := PreviewSkill(SkillImport{SourceText: file.SourceText, ToolPolicy: &auto})
	if err != nil || changed.Skill.ToolPolicy != auto {
		t.Fatalf("portable override: %+v %v", changed, err)
	}
	for _, policy := range []string{"always", "required\n启用：是"} {
		if _, err := PreviewSkill(SkillImport{SourceText: source, ToolPolicy: &policy}); err == nil {
			t.Fatal("invalid policy accepted", policy)
		}
	}
	for _, raw := range []string{source, file.SourceText} {
		empty := []string{"knowledge_search"}
		if _, err := PreviewSkill(SkillImport{SourceText: raw, ToolPolicy: &required, Tools: &empty}); err == nil {
			t.Fatal("required without MCP accepted")
		}
	}
}
