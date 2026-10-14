package knowledge

import (
	"strings"
	"testing"
)

func runeSlice(s string) []rune { return []rune(s) }

func checkInvariants(t *testing.T, body string, chunks []Chunk) {
	t.Helper()
	runes := runeSlice(body)
	for i, c := range chunks {
		if c.Index != i {
			t.Fatalf("chunk %d has index %d", i, c.Index)
		}
		if c.CharStart < 0 || c.CharEnd > len(runes) || c.CharStart >= c.CharEnd {
			t.Fatalf("chunk %d range [%d,%d) invalid for %d runes", i, c.CharStart, c.CharEnd, len(runes))
		}
		if got := string(runes[c.CharStart:c.CharEnd]); got != c.Text {
			t.Fatalf("chunk %d text does not match its offsets", i)
		}
		if len(runeSlice(c.Text)) > AutoMaxLen && c.CharEnd-c.CharStart > AutoMaxLen {
			t.Fatalf("chunk %d exceeds auto window", i)
		}
	}
	// Chunks must be ordered and advancing.
	for i := 1; i < len(chunks); i++ {
		if chunks[i].CharStart <= chunks[i-1].CharStart {
			t.Fatalf("chunk %d does not advance (start %d after %d)", i, chunks[i].CharStart, chunks[i-1].CharStart)
		}
	}
	if len(chunks) > 0 {
		if chunks[0].CharStart != 0 {
			t.Fatalf("first chunk starts at %d", chunks[0].CharStart)
		}
		last := chunks[len(chunks)-1]
		if last.CharEnd != len(runes) {
			t.Fatalf("last chunk ends at %d, body has %d runes", last.CharEnd, len(runes))
		}
	}
}

func TestAutoShortBodySingleChunk(t *testing.T) {
	body := "短文本，一段即可。"
	chunks, err := Chunks(body, "auto", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, body, chunks)
	if len(chunks) != 1 || chunks[0].Text != body {
		t.Fatalf("expected single full chunk, got %+v", chunks)
	}
}

func TestAutoBreaksAtBlankLineOverNewlineOverPeriod(t *testing.T) {
	// Blank line at rune 700, newline at 300: the window must break at the blank line.
	var b strings.Builder
	b.WriteString(strings.Repeat("字", 700))
	b.WriteString("\n\n")
	b.WriteString(strings.Repeat("后", 60))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("尾", 400))
	body := b.String()
	chunks := window(runeSlice(body), 0, AutoMaxLen, AutoOverlap, autoPreferences)
	checkInvariants(t, body, chunks)
	if !strings.HasSuffix(chunks[0].Text, "\n\n") || strings.Contains(chunks[0].Text, "后") {
		t.Fatalf("first chunk did not break after the blank line: %d", chunks[0].CharEnd)
	}

	// Newline only, no blank lines, no periods.
	body2 := strings.Repeat("前", 500) + "\n" + strings.Repeat("后", 500)
	chunks2 := window(runeSlice(body2), 0, AutoMaxLen, AutoOverlap, autoPreferences)
	checkInvariants(t, body2, chunks2)
	if chunks2[0].CharEnd != 501 {
		t.Fatalf("expected break after newline at 501, got %d", chunks2[0].CharEnd)
	}

	// Period only.
	body3 := strings.Repeat("前。", 300) + strings.Repeat("尾", 500)
	chunks3 := window(runeSlice(body3), 0, AutoMaxLen, AutoOverlap, autoPreferences)
	checkInvariants(t, body3, chunks3)
	if !strings.HasSuffix(chunks3[0].Text, "。") {
		t.Fatalf("expected break after a period, got suffix %q", chunks3[0].Text[len(chunks3[0].Text)-3:])
	}

	// No break points at all: hard cut at the maximum length.
	body4 := strings.Repeat("硬", 2000)
	chunks4 := window(runeSlice(body4), 0, AutoMaxLen, AutoOverlap, autoPreferences)
	checkInvariants(t, body4, chunks4)
	if chunks4[0].CharEnd != AutoMaxLen {
		t.Fatalf("expected hard cut at %d, got %d", AutoMaxLen, chunks4[0].CharEnd)
	}
	for _, c := range chunks4 {
		if c.CharEnd-c.CharStart > AutoMaxLen {
			t.Fatal("hard cut exceeded maximum length")
		}
	}
}

func TestAutoOverlapAndCoverage(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString(strings.Repeat(string(rune('一'+i%26)), 40))
		b.WriteString("\n\n")
	}
	body := b.String()
	chunks := window(runeSlice(body), 0, AutoMaxLen, AutoOverlap, autoPreferences)
	checkInvariants(t, body, chunks)
	if len(chunks) < 2 {
		t.Fatal("expected multiple chunks")
	}
	for i := 1; i < len(chunks); i++ {
		prev, cur := chunks[i-1], chunks[i]
		overlap := prev.CharEnd - cur.CharStart
		if overlap < 0 || overlap > AutoOverlap {
			t.Fatalf("chunk %d overlap %d outside (0,%d]", i, overlap, AutoOverlap)
		}
	}
}

func TestAutoIgnoresCustomOptions(t *testing.T) {
	body := strings.Repeat(" https://example.com/x a@b.com \n\n", 40) + strings.Repeat("正", 1200)
	opts := Options{MaxLength: 100, OverlapPercent: 50, Separator: "period", RemoveURLs: true, RemoveEmails: true, CollapseWhitespace: true}
	viaChunks, err := Chunks(body, "auto", opts)
	if err != nil {
		t.Fatal(err)
	}
	direct := window(runeSlice(body), 0, AutoMaxLen, AutoOverlap, autoPreferences)
	if len(viaChunks) != len(direct) {
		t.Fatalf("auto applied custom options: %d vs %d chunks", len(viaChunks), len(direct))
	}
	for i := range direct {
		if viaChunks[i] != direct[i] {
			t.Fatalf("chunk %d differs: %+v vs %+v", i, viaChunks[i], direct[i])
		}
	}
	if !strings.Contains(viaChunks[0].Text, "https://") {
		t.Fatal("auto must not preprocess the text")
	}
	if _, err := Chunks(body, "", opts); err != nil {
		t.Fatal("empty strategy should behave like auto")
	}
}

func TestCustomParametersAndValidation(t *testing.T) {
	// Hard cut: no separators at all.
	body := strings.Repeat("字", 2500)
	chunks, err := Chunks(body, "custom", Options{MaxLength: 100, OverlapPercent: 0, Separator: "newline"})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, body, chunks)
	if len(chunks) != 25 {
		t.Fatalf("expected 25 hard-cut chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.CharEnd-c.CharStart != 100 {
			t.Fatalf("chunk %d length %d != 100", c.Index, c.CharEnd-c.CharStart)
		}
	}

	// Overlap of 10% of 100 runes.
	overlapped, err := Chunks(body, "custom", Options{MaxLength: 100, OverlapPercent: 10, Separator: "period"})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, body, overlapped)
	if overlapped[1].CharStart != 90 {
		t.Fatalf("expected 10-rune overlap, chunk 1 starts at %d", overlapped[1].CharStart)
	}

	// Newline separator breaks after the last newline in the window.
	lines := strings.Repeat(strings.Repeat("行", 50)+"\n", 30)
	sep, err := Chunks(lines, "custom", Options{MaxLength: 120, OverlapPercent: 0, Separator: "newline"})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, lines, sep)
	if !strings.HasSuffix(sep[0].Text, "\n") || len(runeSlice(sep[0].Text)) > 120 {
		t.Fatalf("newline break not applied: %d runes", len(runeSlice(sep[0].Text)))
	}

	for _, bad := range []Options{
		{MaxLength: 99, OverlapPercent: 0, Separator: "newline"},
		{MaxLength: 2001, OverlapPercent: 0, Separator: "newline"},
		{MaxLength: 100, OverlapPercent: 51, Separator: "newline"},
		{MaxLength: 100, OverlapPercent: -1, Separator: "newline"},
		{MaxLength: 100, OverlapPercent: 0, Separator: "comma"},
	} {
		if _, err := Chunks(body, "custom", bad); err == nil {
			t.Fatalf("expected rejection for %+v", bad)
		}
	}
}

func TestCustomPreprocessOffsets(t *testing.T) {
	body := "开头介绍。\n\n详情见 https://example.com/very/long/path 还有 mail user@example.com 可以联系。\n\n" + strings.Repeat("结尾内容。", 100)
	opts := Options{MaxLength: 200, OverlapPercent: 0, Separator: "blank", RemoveURLs: true, RemoveEmails: true, CollapseWhitespace: true}
	chunks, err := Chunks(body, "custom", opts)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the documented preprocessing to verify offset semantics.
	pre := body
	pre = urlRegex.ReplaceAllString(pre, "")
	pre = emailRegex.ReplaceAllString(pre, "")
	pre = spaceCollapseRegex.ReplaceAllString(pre, " ")
	preRunes := runeSlice(pre)
	for i, c := range chunks {
		if c.CharEnd > len(preRunes) || string(preRunes[c.CharStart:c.CharEnd]) != c.Text {
			t.Fatalf("chunk %d offsets do not address the preprocessed text", i)
		}
		if strings.Contains(c.Text, "https://") || strings.Contains(c.Text, "@") {
			t.Fatalf("chunk %d kept preprocessed-away content", i)
		}
	}
}

func TestHierarchySectionsAndWindowing(t *testing.T) {
	long := strings.Repeat("长段落内容。", 200) // 1200 runes, exceeds the auto window
	body := "# 总标题\n\n开头简介。\n\n## 第一节\n\n" + long + "\n\n### 第二节\n\n第二节正文。\n\n#### 四级不算标题\n\n仍属第二节。\n"
	chunks, err := Chunks(body, "hierarchy", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, body, chunks)
	joined := ""
	for _, c := range chunks {
		joined += c.Text + "|"
	}
	for _, heading := range []string{"# 总标题", "## 第一节", "### 第二节"} {
		found := false
		for _, c := range chunks {
			if strings.Contains(c.Text, heading) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("heading %q missing from chunks", heading)
		}
	}
	// The oversized first section must be windowed into several chunks.
	if len(chunks) < 4 {
		t.Fatalf("expected the long section to be windowed, got %d chunks", len(chunks))
	}
	// A chunk must not span two different headings.
	for _, c := range chunks {
		if strings.Contains(c.Text, "# 总标题") && strings.Contains(c.Text, "## 第一节") {
			t.Fatal("chunk spans two sections")
		}
	}
	_ = joined
}

func TestHierarchyPreamble(t *testing.T) {
	body := strings.Repeat("前言。", 10) + "\n# 标题\n正文。"
	chunks, err := Chunks(body, "hierarchy", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, body, chunks)
	if !strings.Contains(chunks[0].Text, "前言。") {
		t.Fatal("preamble section missing")
	}
}

func TestUnsupportedStrategy(t *testing.T) {
	if _, err := Chunks("内容", "semantic", Options{}); err == nil {
		t.Fatal("expected unsupported strategy error")
	}
}
