// Command lm is the Loom command line: it weaves two source layers into one product.
//
// The warp (base) is the upstream layer: kept byte for byte, never broken.
// The weft (self) is your own layer: passed through the warp one shuttle at a time.
// Pull out the weft and the warp is still intact. That property can be checked mechanically,
// and it is the reason this language exists.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/axfor/loom"
)

const usage = `lm — weave two source layers into one product

Usage:
  lm build [-e vars-file] [-o output-dir] [-report file]
                            build the whole tree: weave templates, copy our layer and the upstream
                            files listed in take, expand variables, check that no upstream content
                            was lost, write the output directory and print the build report
  lm check [-e vars-file] [-o output-dir]
                            run the same checks as build without writing anything; with -o, also
                            report files in output-dir that differ from what the sources build
  lm sync <new-upstream-dir>
                            replace the upstream layer with a new upstream release and carry our
                            edits onto it: every merge template's file gets a three-way merge, with
                            conflict markers where upstream changed the lines we changed
  lm weave [-e vars-file] [-stdin] <template.lm>
                            weave one template and write the product to stdout; with -stdin the
                            template text is read from stdin (an unsaved editor buffer)
  lm list [-tsv]            print each template's metadata (for outer gates)
  lm anchors                list the upstream line each anchor resolves to right now
  lm view                   generate a derived view annotated with anchors
  lm version                print the version, platform and Go version

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
	case "version", "-version", "--version":
		printVersion()
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
	c, err := loom.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	switch cmd {
	case "weave":
		fs := flag.NewFlagSet("weave", flag.ContinueOnError)
		envFlag := fs.String("e", "", "variables file (default: lm.e next to loom.lm)")
		stdinFlag := fs.Bool("stdin", false, "read the template text from stdin; the path still names the product")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: lm weave [-e vars-file] [-stdin] <template.lm>")
		}
		if err := loadVars(c, *envFlag); err != nil {
			return err
		}
		out, err := weaveFile(c, fs.Arg(0), *stdinFlag)
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
			if err := os.WriteFile(*reportFlag, []byte(plan.Report.Markdown()), 0o644); err != nil {
				return err
			}
		}
		if cmd == "check" && *outFlag != "" {
			// With -o, check also answers "would lm build change this output directory?". Only on request:
			// while sources are being edited the output is behind by design, and a check of the sources
			// (a lint step before building, say) must not fail for that.
			problems, err := loom.StaleOutputs(c, plan, outDir)
			if err != nil {
				return err
			}
			if len(problems) > 0 {
				for i, p := range problems {
					if i == 20 {
						fmt.Printf("... and %d more\n", len(problems)-20)
						break
					}
					fmt.Println("⛔ " + p)
				}
				return fmt.Errorf("%d files in %s differ from what the sources build — run lm build", len(problems), outDir)
			}
		}
		return nil

	case "sync":
		if len(args) != 1 {
			return fmt.Errorf("usage: lm sync <new-upstream-dir>")
		}
		r, err := loom.Sync(c, args[0])
		if r != nil {
			printSync(r)
		}
		if err != nil {
			return err
		}
		if n := len(r.Conflicts) + len(r.Gone); n > 0 {
			return fmt.Errorf("%d files need you — resolve the conflict markers, or decide what happens to a file upstream removed; then lm build", n)
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

func printSync(r *loom.SyncReport) {
	fmt.Printf("upstream    %4d added · %d changed · %d removed\n", len(r.Added), len(r.Changed), len(r.Removed))
	list := func(title string, paths []string, note string) {
		if len(paths) == 0 {
			return
		}
		fmt.Printf("%-11s %4d files   %s\n", title, len(paths), note)
		for _, p := range paths {
			fmt.Printf("  %s\n", p)
		}
	}
	list("merged", r.Merged, "our edits carried onto the new upstream")
	list("conflicts", r.Conflicts, "conflict markers left in our file")
	list("gone", r.Gone, "upstream removed the file our file is merged into")
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

func weaveFile(c *loom.Config, path string, stdin bool) (string, error) {
	var t *loom.Template
	var err error
	if stdin {
		var src []byte
		if src, err = io.ReadAll(os.Stdin); err != nil {
			return "", err
		}
		t, err = loom.LoadTemplateSource(c, path, src)
	} else {
		t, err = loom.LoadTemplate(c, path)
	}
	if err != nil {
		return "", err
	}
	return loom.Weave(c, t)
}
