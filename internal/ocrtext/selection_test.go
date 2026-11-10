package ocrtext

import (
	"strings"
	"testing"
)

func TestWordBlocksPreserveEntitiesAndRoundTrip(t *testing.T) {
	text := "离线文字识别，支持中文。\nScreenPin 0.2.1 https://example.com/a?q=1 test@example.com 123.45"
	s := New(text, false)
	for _, want := range []string{"离线", "文字", "识别", "ScreenPin", "0.2.1", "https://example.com/a?q=1", "test@example.com", "123.45"} {
		found := false
		for _, token := range s.Tokens {
			found = found || token.Text == want
		}
		if !found {
			t.Errorf("missing word %q: %+v", want, s.Tokens)
		}
	}
	s.All(true)
	if s.SelectedText() != text {
		t.Fatal(s.SelectedText())
	}
	s.SetCharacters(true)
	if s.SelectedText() != text {
		t.Fatal("character mode damaged text")
	}
	s.SetCharacters(false)
	if s.SelectedText() != text {
		t.Fatal("word mode damaged text")
	}
}

func TestSelectionKeepsOrderAndSeparators(t *testing.T) {
	s := New("hello skipped world\n你好，世界", false)
	for i := range s.Tokens {
		if strings.Contains("hello world 世界", s.Tokens[i].Text) {
			s.Tokens[i].Selected = true
		}
	}
	if got := s.SelectedText(); got != "hello world\n世界" {
		t.Fatal(got)
	}
	s.Invert()
	s.Invert()
	if s.SelectedText() != "hello world\n世界" {
		t.Fatal("invert lost selection")
	}
	s.All(false)
	s.Range(2, 0, true)
	if s.SelectedText() != "hello skipped world" {
		t.Fatal(s.SelectedText())
	}
}

func TestChangingGranularityNeverSelectsExtraText(t *testing.T) {
	s := New("文字识别", true)
	s.Tokens[0].Selected = true
	s.SetCharacters(false)
	if s.Count() != 0 {
		t.Fatal("part of a word selected the entire word")
	}
}

func TestScreenshotTermsStayTogether(t *testing.T) {
	s := New("截图之后选词复制，长图也支持离线分词", false)
	for _, word := range []string{"截图", "选词", "长图", "分词"} {
		found := false
		for _, token := range s.Tokens {
			found = found || token.Text == word
		}
		if !found {
			t.Errorf("split screenshot term %q", word)
		}
	}
}

func TestStreamingInsertionKeepsSelectedLine(t *testing.T) {
	old := New("Hello world\n你好世界", false)
	old.Tokens[0].Selected = true
	next := New("新增一行\nHello world\n右边的列\n你好世界", false)
	next.PreserveFrom(old)
	if next.SelectedText() != "Hello" || next.Count() != 1 {
		t.Fatal("streamed insertion lost or expanded selection", next.SelectedText())
	}
	ambiguous := New("Hello world\nHello world\n你好世界", false)
	ambiguous.PreserveFrom(old)
	if ambiguous.Count() != 0 {
		t.Fatal("matched an ambiguous duplicate line")
	}
}

func BenchmarkLongOCRSegmentation(b *testing.B) {
	text := strings.Repeat("ScreenPin 离线文字识别，支持截图之后选词复制。\n", 500)
	New(text, false)
	b.ReportAllocs()
	for b.Loop() {
		New(text, false)
	}
}
