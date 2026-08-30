// Package report renders scan findings as a human table or machine JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/moveeeax/k8s-image-tag-lint/internal/scan"
)

// Table writes findings as an aligned text table. When there are no findings it
// writes a short all-clear line.
func Table(w io.Writer, findings []scan.Finding) error {
	if len(findings) == 0 {
		_, err := fmt.Fprintln(w, "no unpinned images found")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FILE\tKIND\tNAME\tCONTAINER\tIMAGE\tREASON")
	for _, f := range findings {
		name := f.Name
		if f.Namespace != "" {
			name = f.Namespace + "/" + f.Name
		}
		container := f.Container
		if f.Init {
			container = "(init) " + container
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", f.File, f.Kind, name, container, f.Image, f.Reason)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\n%d unpinned image(s) found\n", len(findings))
	return err
}

// JSON writes findings as a JSON array (always an array, never null).
func JSON(w io.Writer, findings []scan.Finding) error {
	if findings == nil {
		findings = []scan.Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(findings)
}
