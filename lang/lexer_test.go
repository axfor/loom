package lang

import (
	"strings"
	"testing"
)

// A single backtick ends at the next backtick, so it cannot hold a document: real markdown is
// full of inline `code` and fenced blocks. Three or more open a fence that ends only at as many
// again, which can.
func TestFencedLiteral(t *testing.T) {
	src := "base.X.after(````markdown\n" +
		"## Where this fits\n" +
		"\n" +
		"Run `lm build`:\n" +
		"\n" +
		"```sh\n" +
		"lm build\n" +
		"```\n" +
		"````)\n"
	toks, err := Lex("t.lm", []byte(src))
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	var raw *Tok
	for i := range toks {
		if toks[i].Kind == KRaw {
			raw = &toks[i]
		}
	}
	if raw == nil {
		t.Fatal("no literal was produced")
	}
	if raw.Tag != "markdown" {
		t.Errorf("tag %q, want markdown", raw.Tag)
	}
	for _, want := range []string{"Run `lm build`:", "```sh", "lm build"} {
		if !strings.Contains(raw.Text, want) {
			t.Errorf("the fence lost %q:\n%s", want, raw.Text)
		}
	}
	if strings.Contains(raw.Text, "````") {
		t.Errorf("the fence itself leaked into the content:\n%s", raw.Text)
	}
	// What follows the closing fence keeps being lexed, so the call still closes.
	closed := false
	for i, tk := range toks {
		if tk.Kind == KRaw && i+1 < len(toks) && toks[i+1].Kind == KRParen {
			closed = true
		}
	}
	if !closed {
		t.Error("the ) after the closing fence was not lexed")
	}
}

// One backtick still means what it meant, so nothing already written changes.
func TestSingleBacktickUnchanged(t *testing.T) {
	toks, err := Lex("t.lm", []byte("base.X.after(`plain text`)\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range toks {
		if tk.Kind == KRaw {
			if tk.Text != "plain text" || tk.Tag != "" {
				t.Errorf("got %q tag %q", tk.Text, tk.Tag)
			}
			return
		}
	}
	t.Error("no literal")
}

func TestUnterminatedFence(t *testing.T) {
	_, err := Lex("t.lm", []byte("base.X.after(```\nnever closed\n"))
	if err == nil || !strings.Contains(err.Error(), "unterminated fence") {
		t.Errorf("want an unterminated-fence error, got %v", err)
	}
}

// A fence is closed by exactly as many backticks as opened it, so a shorter run inside is content.
func TestFenceLengthMatters(t *testing.T) {
	toks, err := Lex("t.lm", []byte("base.X.after(````\na ``` inside\n````)\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range toks {
		if tk.Kind == KRaw {
			if !strings.Contains(tk.Text, "a ``` inside") {
				t.Errorf("a shorter run closed the fence: %q", tk.Text)
			}
			return
		}
	}
	t.Error("no literal")
}
