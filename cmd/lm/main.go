// Command lm is the Loom command line: it weaves two source layers into one product.
//
// The warp (up) is the upstream layer: kept byte for byte, never broken.
// The weft (me) is your own layer: passed through the warp one shuttle at a time.
// Pull out the weft and the warp is still intact. That property can be checked mechanically,
// and it is the reason this language exists.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axfor/loom"
)

const usage = `lm — weave two source layers into one product

Usage:
  lm build [-e vars-file] [-o output-dir] [-report file]
                            build the whole tree: weave templates, copy our layer and the upstream
                            files listed in take, expand variables, check that no upstream content
                            was lost, write the output directory and print the build report
  lm check [-e vars-file]   run the same checks as build without writing the product
  lm weave [-e vars-file] <template.lm>
                            weave one template and write the product to stdout
  lm list [-tsv]            print each template's metadata (for outer gates)
  lm anchors                list the upstream line each anchor resolves to right now
  lm view                   generate a derived view annotated with anchors
  lm migrate [-n]           rewrite a legacy-syntax (HCL) loom.lm and templates into the new
                            syntax; -n lists the plan without touching files

Settings are read from the nearest loom.lm (searching up from the current directory).
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "⛔ %v\n", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfgPath, err := loom.FindConfig(wd)
	if err != nil {
		return err
	}
	if cmd == "migrate" {
		return migrate(cfgPath, args)
	}
	c, err := loom.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	switch cmd {
	case "weave":
		fs := flag.NewFlagSet("weave", flag.ContinueOnError)
		envFlag := fs.String("e", "", "variables file (default: lm.e next to loom.lm)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: lm weave [-e vars-file] <template.lm>")
		}
		if err := loadVars(c, *envFlag); err != nil {
			return err
		}
		out, err := weaveFile(c, fs.Arg(0))
		if err != nil {
			return err
		}
		_, err = os.Stdout.WriteString(out)
		return err

	case "build", "check":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		envFlag := fs.String("e", "", "variables file (default: lm.e next to loom.lm)")
		outFlag := fs.String("o", "", "output directory (default: output in loom.lm)")
		reportFlag := fs.String("report", "", "write the full build report to this file (markdown)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return fmt.Errorf("usage: lm %s [-e vars-file] [-o output-dir] [-report report-file]", cmd)
		}
		if err := loadVars(c, *envFlag); err != nil {
			return err
		}
		outDir := *outFlag
		if outDir == "" && c.Output != "" {
			outDir = filepath.Join(c.Root, c.Output)
		}
		if cmd == "build" && outDir == "" {
			return fmt.Errorf("no output directory — pass -o, or write output \"...\" in loom.lm")
		}
		plan, err := loom.PlanBuild(c, cmd == "build")
		if err != nil {
			return err
		}
		if cmd == "build" {
			if err := loom.WriteBuild(c, plan, outDir); err != nil {
				return err
			}
		}
		plan.Report.Print(os.Stdout)
		if *reportFlag != "" {
			return os.WriteFile(*reportFlag, []byte(plan.Report.Markdown()), 0o644)
		}
		return nil

	case "list":
		if len(args) == 1 && args[0] == "-tsv" {
			return loom.ListTSV(c, os.Stdout)
		}
		return loom.List(c, os.Stdout)
	case "anchors":
		return loom.ListAnchors(c, os.Stdout)
	case "view":
		return loom.AnchoredView(c, os.Stdout)
	}
	return fmt.Errorf("unknown subcommand `%s`\n%s", cmd, usage)
}

// loadVars reads only the file given with -e (no fallback to lm.e); without -e it reads lm.e next to
// loom.lm, and no such file means no variables.
func loadVars(c *loom.Config, file string) error {
	if file == "" {
		p := filepath.Join(c.Root, loom.VarsName)
		if _, err := os.Stat(p); err != nil {
			c.Vars = loom.NoVars()
			return nil
		}
		file = p
	}
	v, err := loom.LoadVars(file)
	if err != nil {
		return err
	}
	c.Vars = v
	return nil
}

func weaveFile(c *loom.Config, path string) (string, error) {
	t, err := loom.LoadTemplate(c, path)
	if err != nil {
		return "", err
	}
	return loom.Weave(c, t)
}

// migrate rewrites the settings and every legacy-syntax template. Everything is rewritten in memory
// before any file is touched: if one can't be rewritten, no file changes, so the repository is never
// left half old and half new.
func migrate(cfgPath string, args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dry := fs.Bool("n", false, "only list the plan; don't touch files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := loom.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	tpls, err := loom.Templates(c)
	if err != nil {
		return err
	}
	var done []*loom.Migrated
	for _, p := range tpls {
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !loom.IsLegacySyntax(src) {
			continue
		}
		m, err := loom.MigrateTemplate(c, p)
		if err != nil {
			return err
		}
		done = append(done, m)
	}
	cfgSrc, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	newCfg := ""
	if strings.Contains(string(cfgSrc), "layer") {
		if newCfg, err = loom.MigrateConfig(cfgPath); err != nil {
			return err
		}
	}

	rel := func(p string) string {
		if r, err := filepath.Rel(c.Root, p); err == nil {
			return r
		}
		return p
	}
	for _, m := range done {
		if m.From != m.To {
			fmt.Printf("  %s → %s\n", rel(m.From), rel(m.To))
		} else {
			fmt.Printf("  %s\n", rel(m.From))
		}
		for _, n := range m.Notes {
			fmt.Printf("      ⚠ %s\n", n)
		}
	}
	if newCfg != "" {
		fmt.Printf("  %s (settings)\n", rel(cfgPath))
	}
	if *dry {
		fmt.Printf("would rewrite %d templates%s (-n: no files touched)\n", len(done), map[bool]string{true: " + settings", false: ""}[newCfg != ""])
		return nil
	}
	for _, m := range done {
		if _, err := os.Stat(m.To); err == nil && m.To != m.From {
			return fmt.Errorf("%s already exists; not overwriting", rel(m.To))
		}
	}
	for _, m := range done {
		if err := os.MkdirAll(filepath.Dir(m.To), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(m.To, []byte(m.Text), 0o644); err != nil {
			return err
		}
		if m.To != m.From {
			if err := os.Remove(m.From); err != nil {
				return err
			}
		}
	}
	if newCfg != "" {
		if err := os.WriteFile(cfgPath, []byte(newCfg), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("✓ rewrote %d templates%s\n", len(done), map[bool]string{true: " + settings", false: ""}[newCfg != ""])
	return nil
}
