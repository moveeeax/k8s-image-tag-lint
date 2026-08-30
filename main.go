// Command k8s-image-tag-lint scans Kubernetes manifests and fails when a
// workload container uses a mutable image tag (:latest, no tag) or, in strict
// mode, is not pinned to a @sha256 digest. It exits non-zero on any violation
// so it can gate CI before an unpinned image ships.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/moveeeax/k8s-image-tag-lint/internal/report"
	"github.com/moveeeax/k8s-image-tag-lint/internal/scan"
)

const usage = `k8s-image-tag-lint — flag mutable Kubernetes image tags

Usage:
  k8s-image-tag-lint scan [flags] <path> [path...]

Flags:
  --require digest   also flag any tag lacking a @sha256 digest (strict)
  --json             emit findings as a JSON array

Exit code is non-zero when any violation is found.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(os.Stderr, usage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "scan":
		return runScan(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runScan(args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	require := fs.String("require", "", `set to "digest" to require a @sha256 digest`)
	asJSON := fs.Bool("json", false, "emit findings as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "scan: need at least one path")
		return 2
	}
	if *require != "" && *require != "digest" {
		fmt.Fprintf(os.Stderr, "scan: --require only accepts \"digest\", got %q\n", *require)
		return 2
	}

	findings, err := scan.Paths(paths, scan.Options{RequireDigest: *require == "digest"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		return 2
	}

	if *asJSON {
		if err := report.JSON(os.Stdout, findings); err != nil {
			fmt.Fprintln(os.Stderr, "scan:", err)
			return 2
		}
	} else {
		if err := report.Table(os.Stdout, findings); err != nil {
			fmt.Fprintln(os.Stderr, "scan:", err)
			return 2
		}
	}

	if len(findings) > 0 {
		return 1
	}
	return 0
}
