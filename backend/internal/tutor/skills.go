package tutor

import (
	"campusclaw/internal/mcp"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Skill struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	TriggerText string   `json:"trigger_text"`
	SubjectText string   `json:"subject_text"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	ToolPolicy  string   `json:"tool_policy"`
	SourceText  string   `json:"source_text"`
	Enabled     bool     `json:"enabled"`
}

var separators = regexp.MustCompile(`[,，、;；|]+`)

func ParseSkill(source string) (Skill, error) {
	if err := validateSource(source); err != nil {
		return Skill{}, err
	}
	if isPortable(source) {
		preview, err := PreviewSkill(SkillImport{SourceText: source})
		if err != nil {
			return Skill{}, err
		}
		if !preview.Ready {
			return Skill{}, errors.New("请设置本应用的适用关键词后再保存")
		}
		return preview.Skill, nil
	}
	return parseCourseSkill(source)
}

func parseCourseSkill(source string) (Skill, error) {
	s := Skill{SourceText: source, Tools: []string{}, ToolPolicy: "auto"}
	normalized := strings.ReplaceAll(strings.TrimPrefix(source, "\ufeff"), "\r\n", "\n")
	fields := map[string]string{}
	var body []string
	started := false
	inBody := false
	for _, line := range strings.Split(normalized, "\n") {
		trimmed := strings.TrimSpace(line)
		if inBody {
			body = append(body, line)
			continue
		}
		if trimmed == "" {
			if started {
				inBody = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(strings.ReplaceAll(trimmed, "：", ":"), ":")
		if !ok {
			return s, errors.New("元数据须使用名称：值格式，并以空行分隔正文")
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "名称", "适用条件", "资料", "工具", "工具策略", "启用":
		default:
			return s, errors.New("未定义的技能字段：" + key)
		}
		if _, exists := fields[key]; exists {
			return s, errors.New("重复的技能字段：" + key)
		}
		fields[key] = value
		started = true
	}
	s.Name = fields["名称"]
	s.TriggerText = fields["适用条件"]
	s.SubjectText = fields["资料"]
	s.Description = strings.TrimSpace(strings.Join(body, "\n"))
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"名称", s.Name, 32}, {"适用条件", s.TriggerText, 120}, {"正文", s.Description, MaxSkillBody}} {
		if field.value == "" || utf8.RuneCountInString(field.value) > field.limit {
			return s, errors.New(field.name + "为空或超过字数限制")
		}
	}
	if utf8.RuneCountInString(s.SubjectText) > 32 {
		return s, errors.New("资料字段不超过32字")
	}
	switch fields["启用"] {
	case "是":
		s.Enabled = true
	case "否":
		s.Enabled = false
	default:
		return s, errors.New("启用为必填项，只接受是或否")
	}
	if fields["工具"] != "" {
		for _, tool := range separators.Split(fields["工具"], -1) {
			tool = strings.TrimSpace(tool)
			if tool != "knowledge_search" {
				_, _, ok := mcp.ParseRef(tool)
				if !ok {
					return s, errors.New("工具须为knowledge_search或小写工具名@服务器名，工具名不含连字符")
				}
			}
			exists := false
			for _, v := range s.Tools {
				if v == tool {
					exists = true
				}
			}
			if !exists {
				s.Tools = append(s.Tools, tool)
			}
		}
	}
	switch fields["工具策略"] {
	case "", "auto":
		s.ToolPolicy = "auto"
	case "required":
		s.ToolPolicy = "required"
	default:
		return s, errors.New("工具策略只接受auto或required")
	}
	if s.ToolPolicy == "required" {
		hasMCP := false
		for _, tool := range s.Tools {
			hasMCP = hasMCP || tool != "knowledge_search"
		}
		if !hasMCP {
			return s, errors.New("必须调用策略至少需要勾选一个MCP工具")
		}
	}
	if len(s.Tools) > 16 {
		return s, errors.New("一个技能最多绑定16个工具")
	}
	if len(triggerWords(s.TriggerText)) == 0 {
		return s, errors.New("适用条件至少包含一个非空关键词")
	}
	return s, nil
}

func triggerWords(text string) []string {
	words := []string{}
	for _, word := range separators.Split(strings.ToLower(text), -1) {
		if word = strings.TrimSpace(word); word != "" {
			words = append(words, word)
		}
	}
	return words
}

// SelectSkill is deterministic: only enabled candidates, lexical score,
// positive score required, then smaller ID. The caller supplies bound skills.
func SelectSkill(skills []Skill, question string) *Skill {
	question = strings.ToLower(question)
	best := 0
	var selected *Skill
	for _, skill := range skills {
		if !skill.Enabled {
			continue
		}
		score := 0
		for _, word := range triggerWords(skill.TriggerText) {
			if strings.Contains(question, word) {
				score++
			}
		}
		if score > best || (score > 0 && score == best && selected != nil && skill.ID < selected.ID) {
			copy := skill
			selected = &copy
			best = score
		}
	}
	return selected
}

func decodeTools(raw string) []string {
	tools := []string{}
	_ = json.Unmarshal([]byte(raw), &tools)
	return tools
}
