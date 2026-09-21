package build_test

// The compiled weaver has to agree with lm build, or the language has two meanings. This compiles
// a template to a native binary, runs it against two different upstreams, and holds its output
// against what weaving the same template in process produces.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

func TestCompiledWeaverAgreesWithTheBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("compiling a weaver runs the Go toolchain")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no Go toolchain to build a weaver with")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nloom \"1.0\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md.lm"), strings.Join([]string{
		`if base.has.Overview {`,
		`    base.Overview.after(self.job)`,
		`}`,
		`base.sections[level == 2 && name ~ "^Other"].demote()`,
		``,
		`---`,
		`Self:`,
		"    ```markdown",
		`    ## job`,
		``,
		`    what it does`,
		"    ```",
	}, "\n")+"\n")

	// The source the compiler emits has to be Go, and the binary has to build.
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "f.md.lm"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := build.Generate(c, tm, "main")
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	// The question is compiled, not carried: an if in the template is an if in the program.
	if !strings.Contains(string(src), "if w.Has(\"heading\", \"Overview\") {") {
		t.Errorf("the condition did not become control flow:\n%s", src)
	}
	if !strings.Contains(string(src), "what it does") {
		t.Error("the resource section was not carried into the program")
	}

	bin := filepath.Join(dir, "weaver")
	cmd := exec.Command(filepath.Join(root, "lm-test-build"), "compile", "-loom", root, "-o", bin, filepath.Join(dir, "me", "f.md.lm"))
	if _, err := os.Stat(cmd.Path); err != nil {
		// Build the command once, into the temp directory, rather than expecting one on PATH.
		lm := filepath.Join(dir, "lm")
		b := exec.Command("go", "build", "-o", lm, "./cmd/lm")
		b.Dir = root
		if out, err := b.CombinedOutput(); err != nil {
			t.Fatalf("building lm: %v\n%s", err, out)
		}
		cmd = exec.Command(lm, "compile", "-loom", root, "-o", bin, filepath.Join(dir, "me", "f.md.lm"))
	}
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compiling the weaver: %v\n%s", err, out)
	}

	// Two upstreams: one the question holds for, one it does not.
	for _, up := range []struct{ name, text string }{
		{"with Overview", "# T\n\n## Overview\n\nup text\n\n## Other\n\no\n"},
		{"without it", "# T\n\n## Summary\n\nchanged\n\n## Other\n\no\n"},
	} {
		mustWrite(t, filepath.Join(dir, "up", "f.md"), up.text)
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "f.md.lm"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := build.Weave(c, tm)
		if err != nil {
			t.Fatalf("%s: weaving in process: %v", up.name, err)
		}
		got, err := exec.Command(bin, "--base", filepath.Join(dir, "up", "f.md")).Output()
		if err != nil {
			t.Fatalf("%s: running the weaver: %v", up.name, err)
		}
		if string(got) != want {
			t.Errorf("%s: the weaver and the build disagree\n--- weaver ---\n%s\n--- build ---\n%s", up.name, got, want)
		}
	}
}
