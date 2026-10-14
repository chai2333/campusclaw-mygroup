// Package knowledge splits material body text into retrieval chunks. Every
// chunk carries rune-offset positions (end-exclusive) relative to the text
// that was actually split, so slicing that text with the reported ranges
// reproduces every chunk exactly.
package knowledge

import (
	"errors"
	"regexp"
)

const (
	// AutoMaxLen and AutoOverlap are the fixed auto strategy parameters.
	AutoMaxLen  = 800
	AutoOverlap = 80
	// MinCustomLen, MaxCustomLen and MaxOverlapPercent bound the custom strategy.
	MinCustomLen    = 100
	MaxCustomLen    = 2000
	MaxOverlapPct   = 50
	maxHeadingLevel = 3
)

// Chunk is one ordered split piece. CharStart and CharEnd are rune offsets
// into the split input text; CharEnd is exclusive.
type Chunk struct {
	Index     int
	Text      string
	CharStart int
	CharEnd   int
}

// Options holds custom strategy parameters. They are ignored for auto.
type Options struct {
	MaxLength          int
	OverlapPercent     int
	Separator          string // "newline", "blank" or "period"
	RemoveURLs         bool
	RemoveEmails       bool
	CollapseWhitespace bool
}

type breakKind int

const (
	breakBlank breakKind = iota
	breakNewline
	breakPeriod
)

var autoPreferences = []breakKind{breakBlank, breakNewline, breakPeriod}

var separatorPreferences = map[string][]breakKind{
	"newline": {breakNewline},
	"blank":   {breakBlank},
	"period":  {breakPeriod},
}

var (
	urlRegex           = regexp.MustCompile(`https?://\S+`)
	emailRegex         = regexp.MustCompile(`[^\s@]+@[^\s@]+`)
	spaceCollapseRegex = regexp.MustCompile(`[^\S\r\n]+`)
)

// Chunks splits body with the named strategy; an empty strategy means auto.
// Custom parameters supplied together with auto are ignored.
func Chunks(body, strategy string, opts Options) ([]Chunk, error) {
	switch strategy {
	case "", "auto":
		return window([]rune(body), 0, AutoMaxLen, AutoOverlap, autoPreferences), nil
	case "custom":
		return custom(body, opts)
	case "hierarchy":
		return hierarchy(body), nil
	default:
		return nil, errors.New("unsupported_strategy")
	}
}

func custom(body string, opts Options) ([]Chunk, error) {
	if opts.MaxLength < MinCustomLen || opts.MaxLength > MaxCustomLen {
		return nil, errors.New("invalid_chunk_length")
	}
	if opts.OverlapPercent < 0 || opts.OverlapPercent > MaxOverlapPct {
		return nil, errors.New("invalid_chunk_overlap")
	}
	prefs, ok := separatorPreferences[opts.Separator]
	if !ok {
		return nil, errors.New("invalid_chunk_separator")
	}
	overlap := opts.MaxLength * opts.OverlapPercent / 100
	return window([]rune(preprocess(body, opts)), 0, opts.MaxLength, overlap, prefs), nil
}

// preprocess returns the indexing input for the custom strategy. Chunk
// offsets from this text describe the preprocessed text, not the raw file.
func preprocess(body string, opts Options) string {
	s := body
	if opts.RemoveURLs {
		s = urlRegex.ReplaceAllString(s, "")
	}
	if opts.RemoveEmails {
		s = emailRegex.ReplaceAllString(s, "")
	}
	if opts.CollapseWhitespace {
		s = spaceCollapseRegex.ReplaceAllString(s, " ")
	}
	return s
}

// hierarchy splits Markdown by #, ## and ### headings. Each heading stays in
// its own section; a section longer than the auto window is split again with
// the auto window rules. Non-heading text before the first heading, when it
// is not blank, forms its own leading section.
func hierarchy(body string) []Chunk {
	runes := []rune(body)
	headings := headingStarts(runes)
	chunks := []Chunk{}
	add := func(start, end int) {
		for _, c := range window(runes[start:end], start, AutoMaxLen, AutoOverlap, autoPreferences) {
			c.Index = len(chunks)
			chunks = append(chunks, c)
		}
	}
	first := 0
	if len(headings) > 0 {
		first = headings[0]
	}
	if hasContent(runes[:first]) {
		add(0, first)
	}
	for i, h := range headings {
		end := len(runes)
		if i+1 < len(headings) {
			end = headings[i+1]
		}
		add(h, end)
	}
	return chunks
}

// headingStarts returns the rune offsets of lines that begin with one to
// three '#' characters followed by whitespace or the end of the line.
func headingStarts(runes []rune) []int {
	starts := []int{}
	lineStart := 0
	for i := 0; i <= len(runes); i++ {
		if i == len(runes) || runes[i] == '\n' {
			if isHeading(runes, lineStart, i) {
				starts = append(starts, lineStart)
			}
			lineStart = i + 1
		}
	}
	return starts
}

func isHeading(runes []rune, start, end int) bool {
	level := 0
	for start+level < end && runes[start+level] == '#' {
		level++
	}
	if level < 1 || level > maxHeadingLevel {
		return false
	}
	return start+level == end || runes[start+level] == ' ' || runes[start+level] == '\t'
}

func hasContent(runes []rune) bool {
	for _, r := range runes {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return true
		}
	}
	return false
}

// window slides over runes producing chunks of at most maxLen runes with
// overlap runes shared between neighbours. It breaks at the last acceptable
// blank line, newline or period (in preference order) that lies beyond the
// overlap zone, and only hard-cuts when no such break point exists.
func window(runes []rune, base, maxLen, overlap int, prefs []breakKind) []Chunk {
	chunks := []Chunk{}
	start := 0
	for start < len(runes) {
		end := start + maxLen
		if end > len(runes) {
			end = len(runes)
		}
		if end < len(runes) {
			if e, ok := findBreak(runes, start, end, overlap, prefs); ok {
				end = e
			}
		}
		chunks = append(chunks, Chunk{
			Index:     len(chunks),
			Text:      string(runes[start:end]),
			CharStart: base + start,
			CharEnd:   base + end,
		})
		if end >= len(runes) {
			break
		}
		next := end - overlap
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return chunks
}

// findBreak looks for the last break point of the preferred kinds whose end
// position lies in (from, to) and lands more than overlap runes into the
// window. It returns the exclusive chunk end for that break.
func findBreak(runes []rune, from, to, overlap int, prefs []breakKind) (int, bool) {
	limit := from + overlap
	for _, kind := range prefs {
		for i := to - 1; i >= from; i-- {
			end := 0
			switch kind {
			case breakBlank:
				if runes[i] != '\n' {
					continue
				}
				j := i - 1
				if j >= from && runes[j] == '\r' {
					j--
				}
				if j < from || runes[j] != '\n' {
					continue
				}
				end = i + 1
			case breakNewline:
				if runes[i] != '\n' && runes[i] != '\r' {
					continue
				}
				end = i + 1
				if runes[i] == '\r' && end < len(runes) && runes[end] == '\n' {
					end++
				}
			case breakPeriod:
				if runes[i] != '。' && runes[i] != '.' {
					continue
				}
				end = i + 1
			}
			if end > limit {
				return end, true
			}
		}
	}
	return 0, false
}
