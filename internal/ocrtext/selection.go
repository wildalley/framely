// Package ocrtext turns recognized text into locally selectable word blocks.
package ocrtext

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed words.txt.gz
var dictionary []byte
var loadWords sync.Once
var words map[string]float64
var totalLog float64

func initWords() {
	words = make(map[string]float64, 60000)
	r, err := gzip.NewReader(bytes.NewReader(dictionary))
	if err != nil {
		return
	}
	defer r.Close()
	s := bufio.NewScanner(r)
	total := 0.0
	for s.Scan() {
		parts := strings.Fields(s.Text())
		if len(parts) != 2 {
			continue
		}
		n, err := strconv.ParseFloat(parts[1], 64)
		if err == nil && n > 0 {
			words[parts[0]] = math.Log(n)
			total += n
		}
	}
	totalLog = math.Log(math.Max(total, 1))
	// Common screenshot terms missing from the frequency-trimmed dictionary.
	for _, word := range []string{"截图", "选词", "贴图", "长图", "分词"} {
		words[word] = math.Log(2000)
	}
}

type Token struct {
	Start, End int
	Text       string
	Selected   bool
}
type Selection struct {
	Text       string
	Tokens     []Token
	Characters bool
}

var entity = regexp.MustCompile(`(?i)^(?:https?://[^\s<>\x{3000}-\x{303f}\x{4e00}-\x{9fff}\x{ff00}-\x{ffef}]+|[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}|[a-z0-9_]+(?:[./:@+\-][a-z0-9_]+)*)`)

func New(text string, characters bool) *Selection {
	s := &Selection{Text: text, Characters: characters}
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		if unicode.IsSpace(r) {
			i += n
			continue
		}
		end := i + n
		if !characters {
			if loc := entity.FindStringIndex(text[i:]); loc != nil {
				end = i + loc[1]
				for end > i && strings.ContainsRune(".,;!?)]}", rune(text[end-1])) {
					end--
				}
			} else if unicode.Is(unicode.Han, r) {
				loadWords.Do(initWords)
				for end < len(text) {
					rr, nn := utf8.DecodeRuneInString(text[end:])
					if !unicode.Is(unicode.Han, rr) {
						break
					}
					end += nn
				}
				for _, span := range splitHan(text[i:end]) {
					a, b := i+span[0], i+span[1]
					s.Tokens = append(s.Tokens, Token{Start: a, End: b, Text: text[a:b]})
				}
				i = end
				continue
			} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
				for end < len(text) {
					rr, nn := utf8.DecodeRuneInString(text[end:])
					if !unicode.IsLetter(rr) && !unicode.IsDigit(rr) && !unicode.IsMark(rr) {
						break
					}
					end += nn
				}
			}
		}
		s.Tokens = append(s.Tokens, Token{Start: i, End: end, Text: text[i:end]})
		i = end
	}
	return s
}

func splitHan(text string) [][2]int {
	positions := make([]int, 0, utf8.RuneCountInString(text)+1)
	for i := range text {
		positions = append(positions, i)
	}
	positions = append(positions, len(text))
	n := len(positions) - 1
	score := make([]float64, n+1)
	next := make([]int, n)
	for i := n - 1; i >= 0; i-- {
		score[i], next[i] = score[i+1]-totalLog, i+1
		for j := i + 2; j <= n && j <= i+8; j++ {
			if frequency, ok := words[text[positions[i]:positions[j]]]; ok {
				v := frequency - totalLog + score[j]
				if v > score[i] {
					score[i], next[i] = v, j
				}
			}
		}
	}
	var spans [][2]int
	for i := 0; i < n; {
		j := next[i]
		spans = append(spans, [2]int{positions[i], positions[j]})
		i = j
	}
	return spans
}

func (s *Selection) Count() int {
	n := 0
	for _, t := range s.Tokens {
		if t.Selected {
			n++
		}
	}
	return n
}
func (s *Selection) All(on bool) {
	for i := range s.Tokens {
		s.Tokens[i].Selected = on
	}
}
func (s *Selection) Invert() {
	for i := range s.Tokens {
		s.Tokens[i].Selected = !s.Tokens[i].Selected
	}
}
func (s *Selection) Range(a, b int, on bool) {
	for i := max(0, min(a, b)); i <= max(a, b) && i < len(s.Tokens); i++ {
		s.Tokens[i].Selected = on
	}
}

// PreserveFrom keeps choices when a later OCR tile inserts lines before
// existing text. Ambiguous duplicate lines are deliberately left unselected.
func (s *Selection) PreserveFrom(old *Selection) {
	if old == nil {
		return
	}
	byStart := make(map[int]Token)
	for _, t := range old.Tokens {
		if t.Selected {
			byStart[t.Start] = t
		}
	}
	if len(byStart) == 0 {
		return
	}
	copyToken := func(t *Token, oldStart int) {
		if before, ok := byStart[oldStart]; ok && before.Text == t.Text {
			t.Selected = true
		}
	}
	if strings.HasPrefix(s.Text, old.Text) {
		for i := range s.Tokens {
			copyToken(&s.Tokens[i], s.Tokens[i].Start)
		}
		return
	}
	type line struct{ start, count int }
	lineMap := func(text string) map[string]line {
		out := make(map[string]line)
		start := 0
		for _, text := range strings.Split(text, "\n") {
			p := out[text]
			p.start, p.count = start, p.count+1
			out[text] = p
			start += len(text) + 1
		}
		return out
	}
	before, after := lineMap(old.Text), lineMap(s.Text)
	start, i := 0, 0
	for _, text := range strings.Split(s.Text, "\n") {
		end := start + len(text)
		oldLine, ok := before[text]
		for i < len(s.Tokens) && s.Tokens[i].Start < end {
			if ok && oldLine.count == 1 && after[text].count == 1 {
				copyToken(&s.Tokens[i], oldLine.start+s.Tokens[i].Start-start)
			}
			i++
		}
		start = end + 1
	}
}

// Re-segment without discarding selection. A word is selected only when all
// its characters were selected, so switching modes never copies extra text.
func (s *Selection) SetCharacters(characters bool) {
	if s.Characters == characters {
		return
	}
	old := s.Tokens
	next := New(s.Text, characters)
	i := 0
	for j := range next.Tokens {
		t := &next.Tokens[j]
		for i < len(old) && old[i].End <= t.Start {
			i++
		}
		selected := true
		for k := i; k < len(old) && old[k].Start < t.End; k++ {
			if !old[k].Selected {
				selected = false
				break
			}
		}
		t.Selected = selected
	}
	*s = *next
}

func (s *Selection) SelectedText() string {
	if len(s.Tokens) > 0 && s.Count() == len(s.Tokens) {
		return s.Text
	}
	var b strings.Builder
	previous := -1
	for i, t := range s.Tokens {
		if !t.Selected {
			continue
		}
		if previous >= 0 {
			p := s.Tokens[previous]
			gap := s.Text[p.End:t.Start]
			if i == previous+1 {
				b.WriteString(gap)
			} else if strings.ContainsAny(gap, "\r\n") {
				b.WriteByte('\n')
			} else if latinEdge(p.Text, false) && latinEdge(t.Text, true) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t.Text)
		previous = i
	}
	return b.String()
}

func latinEdge(s string, first bool) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	if first {
		r, _ = utf8.DecodeRuneInString(s)
	}
	return !unicode.Is(unicode.Han, r) && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// Rows bound the number of live UI controls even for very large OCR results.
func (s *Selection) Rows() [][2]int {
	var rows [][2]int
	start := 0
	for i, t := range s.Tokens {
		if i > start && (i-start >= 24 || strings.ContainsAny(s.Text[s.Tokens[i-1].End:t.Start], "\r\n")) {
			rows = append(rows, [2]int{start, i})
			start = i
		}
	}
	if start < len(s.Tokens) {
		rows = append(rows, [2]int{start, len(s.Tokens)})
	}
	return rows
}
