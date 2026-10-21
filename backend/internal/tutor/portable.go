package tutor

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const MaxSkillSource = 16000
const MaxSkillBytes = 60000
const MaxSkillBody = 12000

type SkillImport struct {
	SourceText  string    `json:"source_text"`
	Name        *string   `json:"name,omitempty"`
	TriggerText *string   `json:"trigger_text,omitempty"`
	SubjectText *string   `json:"subject_text,omitempty"`
	Tools       *[]string `json:"tools,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
}
type SkillPreview struct {
	Format   string   `json:"format"`
	Skill    Skill    `json:"skill"`
	Warnings []string `json:"warnings"`
	Ready    bool     `json:"ready"`
}
type SkillExport struct {
	Filename   string   `json:"filename"`
	Directory  string   `json:"directory"`
	SourceText string   `json:"source_text"`
	Warnings   []string `json:"warnings"`
}

var portableName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var localResources = regexp.MustCompile(`(?i)(scripts/|references/|assets/|\]\((\./|\.\./|[^:)\s]+\.(md|txt|py|sh|json|csv|yaml|yml)([)#\s])))`)

func validateSource(source string) error {
	if !utf8.ValidString(source) || utf8.RuneCountInString(source) > MaxSkillSource || len(source) > MaxSkillBytes {
		return errors.New("技能文件须为UTF-8，最多16000字符且不超过60000字节")
	}
	return nil
}
func normalize(source string) string {
	return strings.ReplaceAll(strings.TrimPrefix(source, "\ufeff"), "\r\n", "\n")
}
func isPortable(source string) bool { return strings.HasPrefix(normalize(source), "---\n") }

// No aliases, merge keys or custom tags: uploaded metadata is data, never a
// constructor or a source of permissions. Bound nesting before decoding maps.
func checkYAML(n *yaml.Node, depth int, count *int) error {
	*count++
	if depth > 16 || *count > 2048 || n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errors.New("YAML不支持别名、锚点或过深/过大的结构")
	}
	if n.Tag != "" && !strings.HasPrefix(n.Tag, "!!") {
		return errors.New("YAML不支持自定义标签")
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] {
				return errors.New("YAML字段须为不重复的字符串")
			}
			seen[key.Value] = true
		}
	}
	for _, c := range n.Content {
		if err := checkYAML(c, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}
func portableDocument(source string) (map[string]any, string, error) {
	normalized := normalize(source)
	lines := strings.Split(normalized, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, "", errors.New("SKILL.md缺少YAML头部结束分隔线---")
	}
	decoder := yaml.NewDecoder(strings.NewReader(strings.Join(lines[1:end], "\n")))
	var root yaml.Node
	if decoder.Decode(&root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, "", errors.New("SKILL.md头部须为有效YAML对象")
	}
	count := 0
	if err := checkYAML(&root, 0, &count); err != nil {
		return nil, "", err
	}
	var rest yaml.Node
	if decoder.Decode(&rest) != io.EOF {
		return nil, "", errors.New("YAML头部只允许一个文档")
	}
	fields := map[string]any{}
	if root.Decode(&fields) != nil {
		return nil, "", errors.New("YAML字段无效")
	}
	name, ok := fields["name"].(string)
	if !ok || len(name) > 64 || !portableName.MatchString(name) {
		return nil, "", errors.New("标准name须为1–64个小写字母、数字或连字符，不得首尾或连续使用连字符")
	}
	desc, ok := fields["description"].(string)
	if !ok || strings.TrimSpace(desc) == "" || utf8.RuneCountInString(desc) > 1024 {
		return nil, "", errors.New("标准description须为1–1024字符的说明")
	}
	if compatibility, exists := fields["compatibility"]; exists {
		if value, ok := compatibility.(string); !ok || len([]rune(value)) > 500 {
			return nil, "", errors.New("compatibility须为不超过500字符的字符串")
		}
	}
	if raw, exists := fields["metadata"]; exists {
		metadata, ok := raw.(map[string]any)
		if !ok {
			return nil, "", errors.New("metadata须为字符串键值对象")
		}
		for _, v := range metadata {
			if _, ok := v.(string); !ok {
				return nil, "", errors.New("metadata中的值须为字符串，请为数字和布尔值加引号")
			}
		}
	}
	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	if body == "" || utf8.RuneCountInString(body) > MaxSkillBody {
		return nil, "", errors.New("技能正文须为1–12000字符")
	}
	return fields, body, nil
}
func metadataOf(fields map[string]any) map[string]any {
	metadata, _ := fields["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
		fields["metadata"] = metadata
	}
	return metadata
}
func metaString(metadata map[string]any, key string) string {
	value, _ := metadata["campusclaw-"+key].(string)
	return value
}
func applySettings(skill *Skill, input SkillImport) {
	if input.Name != nil {
		skill.Name = strings.TrimSpace(*input.Name)
	}
	if input.TriggerText != nil {
		skill.TriggerText = strings.TrimSpace(*input.TriggerText)
	}
	if input.SubjectText != nil {
		skill.SubjectText = strings.TrimSpace(*input.SubjectText)
	}
	if input.Tools != nil {
		skill.Tools = *input.Tools
	}
	if input.Enabled != nil {
		skill.Enabled = *input.Enabled
	}
}
func validateLocalLines(skill Skill) error {
	for _, value := range append([]string{skill.Name, skill.TriggerText, skill.SubjectText}, skill.Tools...) {
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("名称、关键词、资料和工具字段不可换行")
		}
	}
	return nil
}
func courseSource(s Skill) string {
	enabled := "否"
	if s.Enabled {
		enabled = "是"
	}
	return fmt.Sprintf("名称：%s\n适用条件：%s\n资料：%s\n工具：%s\n启用：%s\n\n%s", s.Name, s.TriggerText, s.SubjectText, strings.Join(s.Tools, ","), enabled, s.Description)
}
func renderPortable(fields map[string]any, s Skill) (string, error) {
	m := metadataOf(fields)
	for key, value := range map[string]string{"display-name": s.Name, "triggers": s.TriggerText, "subject": s.SubjectText, "tools": strings.Join(s.Tools, ","), "enabled": strconv.FormatBool(s.Enabled)} {
		m["campusclaw-"+key] = value
	}
	raw, err := yaml.Marshal(fields)
	if err != nil {
		return "", errors.New("无法序列化技能头部")
	}
	source := "---\n" + string(raw) + "---\n\n" + s.Description + "\n"
	if err = validateSource(source); err != nil {
		return "", err
	}
	return source, nil
}
func warningsFor(fields map[string]any, body string, tools []string) []string {
	warnings := []string{"仅交换自包含SKILL.md；本应用不安装或执行脚本、附件及外部运行环境。"}
	if _, ok := fields["allowed-tools"]; ok {
		warnings = append(warnings, "allowed-tools已保留，但不授予本应用权限；需要的功能须由教师另行配置本班MCP工具。")
	}
	if localResources.MatchString(body) {
		warnings = append(warnings, "正文引用了本地文件或资源目录，单文件导入不包含这些文件；请合并必要文本或适配为MCP工具后使用。")
	}
	if value, ok := fields["compatibility"].(string); ok {
		warnings = append(warnings, "原技能环境要求："+value)
	}
	if len(tools) > 0 {
		warnings = append(warnings, "工具引用只保留名称，不包含服务器配置或令牌；保存时仍须通过本班工具校验。")
	}
	return warnings
}

func PreviewSkill(input SkillImport) (SkillPreview, error) {
	if err := validateSource(input.SourceText); err != nil {
		return SkillPreview{}, err
	}
	if !isPortable(input.SourceText) {
		skill, err := parseCourseSkill(input.SourceText)
		if err != nil {
			return SkillPreview{}, err
		}
		applySettings(&skill, input)
		if err = validateLocalLines(skill); err != nil {
			return SkillPreview{}, err
		}
		skill, err = parseCourseSkill(courseSource(skill))
		if err != nil {
			return SkillPreview{}, err
		}
		return SkillPreview{Format: "course", Skill: skill, Warnings: []string{}, Ready: true}, nil
	}
	fields, body, err := portableDocument(input.SourceText)
	if err != nil {
		return SkillPreview{}, err
	}
	m := metadataOf(fields)
	name := metaString(m, "display-name")
	if name == "" {
		name = fields["name"].(string)
		if len(name) > 32 {
			name = name[:32]
		}
	}
	enabled := false
	if value := metaString(m, "enabled"); value != "" {
		var e error
		enabled, e = strconv.ParseBool(value)
		if e != nil {
			return SkillPreview{}, errors.New("campusclaw-enabled只接受字符串true或false")
		}
	}
	skill := Skill{Name: name, TriggerText: metaString(m, "triggers"), SubjectText: metaString(m, "subject"), Description: body, Tools: []string{}, Enabled: enabled}
	if value := metaString(m, "tools"); value != "" {
		skill.Tools = separators.Split(value, -1)
	}
	applySettings(&skill, input)
	if err = validateLocalLines(skill); err != nil {
		return SkillPreview{}, err
	}
	// Reuse course validation for all local fields, even during partial preview.
	ready := len(triggerWords(skill.TriggerText)) > 0
	validate := skill
	if !ready {
		validate.TriggerText = "待配置"
	}
	checked, err := parseCourseSkill(courseSource(validate))
	if err != nil {
		return SkillPreview{}, err
	}
	skill.Tools = checked.Tools
	skill.SourceText, err = renderPortable(fields, skill)
	if err != nil {
		return SkillPreview{}, err
	}
	warnings := warningsFor(fields, body, skill.Tools)
	if !ready {
		warnings = append(warnings, "请填写本应用的适用关键词（如作文,essay,语法），本应用按关键词匹配技能。")
	}
	return SkillPreview{Format: "agent-skills", Skill: skill, Warnings: warnings, Ready: ready}, nil
}

func ExportSkill(skill Skill) (SkillExport, error) {
	fields := map[string]any{}
	if isPortable(skill.SourceText) {
		var err error
		fields, _, err = portableDocument(skill.SourceText)
		if err != nil {
			return SkillExport{}, err
		}
	} else {
		name := strings.ToLower(skill.Name)
		if len(name) > 64 || !portableName.MatchString(name) {
			hash := sha256.Sum256([]byte(skill.Name))
			name = fmt.Sprintf("campusclaw-skill-%x", hash[:6])
		}
		fields["name"] = name
		fields["description"] = skill.Name + "。适用问题：" + skill.TriggerText + "。"
	}
	source, err := renderPortable(fields, skill)
	if err != nil {
		return SkillExport{}, err
	}
	return SkillExport{Filename: "SKILL.md", Directory: fields["name"].(string), SourceText: source, Warnings: warningsFor(fields, skill.Description, skill.Tools)}, nil
}
