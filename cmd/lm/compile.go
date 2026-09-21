package main

// lm compile: turn a template into a native weaver.
//
// The template's control flow becomes Go control flow and its statements become values the runtime
// places, so the binary decides for itself what to write and shares the engine that writes it.
// What it takes at run time is upstream; what it gives back is the product.

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

func cmdCompile(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	out := fs.String("o", "", "where to write the weaver (default: the product's name)")
	emit := fs.String("emit", "binary", "what to produce: binary or go")
	loomSrc := fs.String("loom", os.Getenv("LOOM_SRC"), "the loom source to build the weaver against (a development lm needs this)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: lm compile [-o weaver] [-emit go] <template.lm>")
	}
	path, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	om, err := lang.FindConfig(filepath.Dir(path))
	if err != nil {
		return err
	}
	c, err := lang.LoadConfig(om)
	if err != nil {
		return err
	}
	t, err := lang.LoadTemplate(c, path)
	if err != nil {
		return err
	}
	src, err := build.Generate(c, t, "main")
	if err != nil {
		return err
	}
	if *emit == "go" {
		if *out == "" {
			os.Stdout.Write(src)
			return nil
		}
		return os.WriteFile(*out, src, 0o644)
	}
	if *emit != "binary" {
		return fmt.Errorf("-emit takes binary or go, not %q", *emit)
	}
	bin := *out
	if bin == "" {
		bin = strings.TrimSuffix(filepath.Base(t.Target), filepath.Ext(t.Target))
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "lm-compile-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		return err
	}
	// The weaver links the very engine that compiled it, so a binary cannot weave differently
	// from the lm that made it. Which engine that is comes from this lm's own build information:
	// a released lm pins the version it was built from, a development one points at its source.
	mod, err := runtimeModule(*loomSrc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		return err
	}
	// The generated module needs its own sum entries; a tidy in the temp directory writes them
	// without touching anything the author has.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Stderr = os.Stderr
	if err := tidy.Run(); err != nil {
		return fmt.Errorf("preparing the weaver's module: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building the weaver: %v", err)
	}
	fmt.Printf("%s → %s\n", lang.Rel(c, path), bin)
	return nil
}

// runtimeModule is the go.mod the weaver is built with. The version is this lm's own, so the
// weaver and the lm that wrote it share one engine; where lm was built from source rather than
// from a release, the source has to be named.
func runtimeModule(src string) (string, error) {
	const path = "github.com/axfor/loom"
	version := ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		if bi.Main.Path == path && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
		for _, d := range bi.Deps {
			if d.Path == path && d.Version != "" {
				version = d.Version
			}
		}
	}
	mod := "module weaver\n\ngo 1.21\n\nrequire " + path + " "
	switch {
	case src != "":
		abs, err := filepath.Abs(src)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err != nil {
			return "", fmt.Errorf("%s is not a loom source tree (no go.mod)", abs)
		}
		return mod + "v0.0.0\n\nreplace " + path + " => " + abs + "\n", nil
	case version != "":
		return mod + version + "\n", nil
	}
	return "", fmt.Errorf("this lm was built from source, so the weaver has no released engine to link — say where the source is: lm compile -loom /path/to/loom, or set LOOM_SRC")
}
