// Command k8s-image-tag-lint scans Kubernetes manifests and fails when a
// workload container uses a mutable image tag (:latest, no tag) or, in strict
// mode, is not pinned to a @sha256 digest. It exits non-zero on any violation
// so it can gate CI before an unpinned image ships.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moveeeax/k8s-image-tag-lint/internal/inventory"
	"github.com/moveeeax/k8s-image-tag-lint/internal/registry"
	"github.com/moveeeax/k8s-image-tag-lint/internal/report"
	"github.com/moveeeax/k8s-image-tag-lint/internal/scan"
)

const usage = `k8s-image-tag-lint — flag mutable Kubernetes image tags

Usage:
  k8s-image-tag-lint scan [flags] <path> [path...]
  kubectl get pods -A -o json | k8s-image-tag-lint inventory [flags] [file|-]

Scan flags:
  --require digest   also flag any tag lacking a @sha256 digest (strict)
  --json             emit findings as a JSON array

Scan exits non-zero when any violation is found.

Inventory flags:
  --output FORMAT    table (default), json or csv
  --check-digests    resolve each tag in its registry, report tags that moved
                     away from the running digest, and image build age
  --platform OS/ARCH image picked from a multi-platform index for build age
                     (default linux/amd64)
  --timeout DURATION limit for all registry lookups (default 2m)

Inventory exits 1 when --check-digests finds a moved tag, 0 otherwise.
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
	case "inventory":
		return runInventory(args[1:], os.Stdin, os.Stdout, registryFor)
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

// registryFor builds the registry resolver used by inventory --check-digests.
// Tests replace it with an in-process registry.
func registryFor(p v1.Platform) inventory.Resolver { return registry.New(p) }

func runInventory(args []string, stdin io.Reader, stdout io.Writer, newResolver func(v1.Platform) inventory.Resolver) int {
	fs := flag.NewFlagSet("inventory", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("output", "table", "table, json or csv")
	check := fs.Bool("check-digests", false, "resolve tags in their registry and report moved tags and build age")
	platform := fs.String("platform", "linux/amd64", "platform picked from a multi-platform index for build age")
	timeout := fs.Duration("timeout", 2*time.Minute, "limit for all registry lookups")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	switch *output {
	case "table", "json", "csv":
	default:
		fmt.Fprintf(os.Stderr, "inventory: --output must be table, json or csv, got %q\n", *output)
		return 2
	}
	plat, err := v1.ParsePlatform(*platform)
	if err == nil && (plat.OS == "" || plat.Architecture == "") {
		err = fmt.Errorf("want OS/ARCH[/VARIANT], got %q", *platform)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "inventory: --platform:", err)
		return 2
	}

	in := stdin
	switch fs.NArg() {
	case 0:
	case 1:
		if p := fs.Arg(0); p != "-" {
			f, err := os.Open(p)
			if err != nil {
				fmt.Fprintln(os.Stderr, "inventory:", err)
				return 2
			}
			defer f.Close()
			in = f
		}
	default:
		fmt.Fprintln(os.Stderr, "inventory: takes at most one file (or - for stdin)")
		return 2
	}

	rows, err := inventory.Read(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "inventory:", err)
		return 2
	}

	if *check {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		inventory.Check(ctx, rows, newResolver(*plat), time.Now())
	}

	switch *output {
	case "json":
		err = report.InventoryJSON(stdout, rows)
	case "csv":
		err = report.InventoryCSV(stdout, rows)
	default:
		err = report.InventoryTable(stdout, rows, *check)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "inventory:", err)
		return 2
	}

	for _, r := range rows {
		if r.CheckError != "" {
			fmt.Fprintf(os.Stderr, "inventory: %s/%s %s: %s\n", r.Namespace, r.Pod, r.Container, r.CheckError)
		}
	}
	for _, r := range rows {
		if r.Moved {
			return 1
		}
	}
	return 0
}
