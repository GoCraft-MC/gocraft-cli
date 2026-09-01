// Command gocraft-cli builds and validates GoCraft plugin bundles.
//
// It is a separate module from the server on purpose: a plugin author compiles,
// they never run a server, and a build tool that could reach into a server's
// internals would eventually do it. Here it cannot — the server is not on the
// module graph, so the rule is enforced by the compiler rather than by review.
//
// The bundle format and its validation are not reimplemented here. They come
// from gocraft-abi, which the server reads them with too, so a bundle that
// builds is a bundle that loads: a tree this refuses is a tree the server would
// have refused, on the machine that has the source.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is overridden at build time via -ldflags, like the server binary.
var version = "dev"

// Exit codes are distinct so a build script can tell a bad invocation from a
// plugin that genuinely failed.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run holds the dispatch so commands can be exercised without spawning a
// process, and so every write goes through an injected writer rather than
// straight to the standard streams.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "build":
		return buildCommand(args[1:], stdout, stderr)
	case "validate":
		return validateCommand(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "gocraft-cli", version)
		return exitOK
	case "help", "-h", "--help":
		usage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		usage(stderr)
		return exitUsage
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `gocraft-cli - build and validate GoCraft plugin bundles

Usage:
  gocraft-cli <command> [arguments]

Commands:
  build <dir>      Pack a plugin source directory into a .gcpkg bundle
  validate <dir>   Check the plugin.toml of a plugin source directory
  version          Print the tool version
  help             Print this message
`)
}
