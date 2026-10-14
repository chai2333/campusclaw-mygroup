package conversation

import "strings"

// citationStripper spans provider chunks, so "[", "12", "]" never reaches a
// zero-evidence answer as a fabricated citation. Other bracket text survives.
type citationStripper struct{ pending string }

func (s *citationStripper) Feed(text string) string {
	var out strings.Builder
	for _, r := range text {
		if s.pending != "" {
			if r >= '0' && r <= '9' {
				s.pending += string(r)
				continue
			}
			if r == ']' && len(s.pending) > 1 {
				s.pending = ""
				continue
			}
			out.WriteString(s.pending)
			s.pending = ""
		}
		if r == '[' {
			s.pending = "["
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}
func (s *citationStripper) Finish() string { v := s.pending; s.pending = ""; return v }
