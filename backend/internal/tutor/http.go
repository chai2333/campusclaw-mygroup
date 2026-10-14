package tutor

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"campusclaw/internal/auth"
)

func idOf(r *http.Request) int64 { id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return id }
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		auth.Error(w, 404, "not_found")
	case errors.Is(err, ErrInvalid):
		auth.Error(w, 400, "invalid_agent")
	case errors.Is(err, ErrDuplicate):
		auth.JSON(w, 400, map[string]string{"error": "duplicate_name", "message": "本班已有同名配置，请更换名称"})
	default:
		auth.Error(w, 503, "service_unavailable")
	}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if dec.Decode(target) != nil {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	var rest any
	if dec.Decode(&rest) != io.EOF {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	return true
}

// @Route GET /api/agents
// @Summary 列出本班智能体（学生只见启用者）
// @Role class
// @Response 200 Agents
func (s *Service) Agents(w http.ResponseWriter, r *http.Request) {
	u := auth.Current(r)
	list, err := s.ListAgents(r.Context(), *u.ClassID, u.Role == "teacher")
	if err != nil {
		fail(w, err)
		return
	}
	auth.JSON(w, 200, map[string]any{"agents": list})
}

// @Route GET /api/agents/{id}
// @Summary 读取本班智能体（学生不可读取停用者）
// @Role class
// @Response 200 Agent
// @Error 404 not_found
func (s *Service) Agent(w http.ResponseWriter, r *http.Request) {
	u := auth.Current(r)
	a, err := LoadAgent(r.Context(), s.DB, *u.ClassID, idOf(r))
	if err != nil {
		fail(w, err)
		return
	}
	if u.Role != "teacher" {
		if !a.Enabled {
			fail(w, ErrNotFound)
			return
		}
		a.Prompt = ""
	}
	auth.JSON(w, 200, a)
}

// @Route POST /api/agents
// @Summary 新建智能体并原子绑定本班技能和材料
// @Role teacher
// @Request AgentInput
// @Response 201 Agent
// @Error 400 invalid_agent / duplicate_name / invalid_request_body
// @Error 404 not_found
func (s *Service) CreateAgent(w http.ResponseWriter, r *http.Request) { s.putAgent(w, r, 0, 201) }

// @Route PUT /api/agents/{id}
// @Summary 更新智能体、启停状态与两份绑定（下一轮生效）
// @Role teacher
// @Request AgentInput
// @Response 200 Agent
// @Error 400 invalid_agent / duplicate_name / invalid_request_body
// @Error 404 not_found
func (s *Service) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	if idOf(r) <= 0 {
		fail(w, ErrNotFound)
		return
	}
	s.putAgent(w, r, idOf(r), 200)
}
func (s *Service) putAgent(w http.ResponseWriter, r *http.Request, id int64, status int) {
	var input AgentInput
	if !decode(w, r, &input) {
		return
	}
	a, err := s.SaveAgent(r.Context(), *auth.Current(r).ClassID, id, input)
	if err != nil {
		fail(w, err)
		return
	}
	auth.JSON(w, status, a)
}

// @Route GET /api/skills
// @Summary 列出本班技能
// @Role class
// @Response 200 Skills
func (s *Service) Skills(w http.ResponseWriter, r *http.Request) {
	list, err := s.ListSkills(r.Context(), *auth.Current(r).ClassID)
	if err != nil {
		fail(w, err)
		return
	}
	auth.JSON(w, 200, map[string]any{"skills": list})
}

// @Route GET /api/skills/{id}
// @Summary 读取技能解析结果与原文
// @Role class
// @Response 200 Skill
// @Error 404 not_found
func (s *Service) GetSkill(w http.ResponseWriter, r *http.Request) {
	skill, err := s.Skill(r.Context(), *auth.Current(r).ClassID, idOf(r))
	if err != nil {
		fail(w, err)
		return
	}
	auth.JSON(w, 200, skill)
}

// @Route POST /api/skills
// @Summary 新建技能：JSON原文或UTF-8文件上传
// @Role teacher
// @Request SkillInput
// @Multipart file
// @Response 201 Skill
// @Error 400 invalid_skill / duplicate_name / invalid_request_body
func (s *Service) CreateSkill(w http.ResponseWriter, r *http.Request) { s.putSkill(w, r, 0, 201) }

// @Route PUT /api/skills/{id}
// @Summary 更新技能原文（下一轮生效）
// @Role teacher
// @Request SkillInput
// @Response 200 Skill
// @Error 400 invalid_skill / duplicate_name / invalid_request_body
// @Error 404 not_found
func (s *Service) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Skill(r.Context(), *auth.Current(r).ClassID, idOf(r)); err != nil {
		fail(w, err)
		return
	}
	s.putSkill(w, r, idOf(r), 200)
}
func (s *Service) putSkill(w http.ResponseWriter, r *http.Request, id int64, status int) {
	var input struct {
		SourceText string `json:"source_text"`
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if id == 0 && media == "multipart/form-data" {
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		if err := r.ParseMultipartForm(64 * 1024); err != nil {
			auth.Error(w, 400, "invalid_request_body")
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		if len(r.MultipartForm.File["file"]) != 1 {
			auth.Error(w, 400, "invalid_request_body")
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			auth.Error(w, 400, "invalid_request_body")
			return
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 32001))
		if err != nil || len(raw) > 32000 {
			auth.Error(w, 400, "invalid_skill")
			return
		}
		input.SourceText = string(raw)
	} else if !decode(w, r, &input) {
		return
	}
	skill, err := ParseSkill(input.SourceText)
	if err != nil {
		auth.JSON(w, 400, map[string]string{"error": "invalid_skill", "message": err.Error()})
		return
	}
	skill, err = s.SaveSkill(r.Context(), *auth.Current(r).ClassID, id, skill)
	if err != nil {
		fail(w, err)
		return
	}
	auth.JSON(w, status, skill)
}

// @Route DELETE /api/skills/{id}
// @Summary 删除本班技能并清理所有绑定
// @Role teacher
// @Response 200 Status
// @Error 404 not_found
func (s *Service) RemoveSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.DeleteSkill(r.Context(), *auth.Current(r).ClassID, idOf(r)); err != nil {
		fail(w, err)
		return
	}
	auth.JSON(w, 200, map[string]string{"status": "ok"})
}
