package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// version is stamped into a release archive with -ldflags "-X main.version=v0.7.1". Installed with
// `go install github.com/axfor/loom/cmd/lm@v0.7.1` it is empty, and the module version from the
// build info says the same thing; built from a checkout, neither knows, and that is "dev".
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// printVersion says the version, and what it was built for: a tool that fetches a binary per
// platform should be able to see which one it got.
func printVersion() {
	fmt.Printf("lm %s %s/%s %s\n", versionString(), runtime.GOOS, runtime.GOARCH, runtime.Version())
}
