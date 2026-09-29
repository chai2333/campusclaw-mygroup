package knowledge

import (
	"strings"
	"testing"
)

func TestReconstruction(t *testing.T) {
	input := strings.Repeat("中文\n\n  text\t", 1000)
	chunks := Split(input, 2000)
	if strings.Join(chunks, "") != input || len(chunks) < 2 {
		t.Fatal("content lost")
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > 2000 {
			t.Fatal("oversized chunk")
		}
	}
}
