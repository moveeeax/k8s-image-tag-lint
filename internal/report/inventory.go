package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/moveeeax/k8s-image-tag-lint/internal/inventory"
)

// InventoryTable writes inventory rows as an aligned text table followed by a
// per-registry container count. The registry check columns (CURRENT, MOVED,
// AGE) are printed only when checked is true. Digests are shortened to 12 hex
// characters; use JSON or CSV for full values.
func InventoryTable(w io.Writer, rows []inventory.Row, checked bool) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "no containers found")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := "REGISTRY\tPOD\tCONTAINER\tREPOSITORY\tTAG\tMUTABILITY\tRUNNING"
	if checked {
		header += "\tCURRENT\tMOVED\tAGE"
	}
	fmt.Fprintln(tw, header)
	for _, r := range rows {
		container := r.Container
		if r.Type != inventory.TypeContainer {
			container = "(" + r.Type + ") " + container
		}
		tag := r.Tag
		if tag == "" {
			tag = "-"
		}
		line := fmt.Sprintf("%s\t%s/%s\t%s\t%s\t%s\t%s\t%s",
			dash(r.Registry), r.Namespace, r.Pod, container, r.Repository, tag, r.Mutability, short(r.RunningDigest))
		if checked {
			moved := "-"
			switch {
			case r.Moved:
				moved = "YES"
			case r.CurrentDigest != "" && r.RunningDigest != "":
				moved = "no"
			case r.CheckError != "":
				moved = "error"
			}
			age := "-"
			if r.AgeDays != nil {
				age = strconv.Itoa(*r.AgeDays) + "d"
			}
			line += fmt.Sprintf("\t%s\t%s\t%s", short(r.CurrentDigest), moved, age)
		}
		fmt.Fprintln(tw, line)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	// Rows arrive sorted by registry, so counting runs is enough.
	fmt.Fprintln(w)
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && rows[j].Registry == rows[i].Registry {
			j++
		}
		fmt.Fprintf(w, "%s: %d container(s)\n", dash(rows[i].Registry), j-i)
		i = j
	}
	if checked {
		moved, failed := 0, 0
		for _, r := range rows {
			if r.Moved {
				moved++
			}
			if r.CheckError != "" {
				failed++
			}
		}
		fmt.Fprintf(w, "%d container(s) run a digest their tag no longer points to\n", moved)
		if failed > 0 {
			fmt.Fprintf(w, "%d container(s) could not be checked (see --output json for errors)\n", failed)
		}
	}
	return nil
}

// InventoryJSON writes rows as a JSON array (always an array, never null).
func InventoryJSON(w io.Writer, rows []inventory.Row) error {
	if rows == nil {
		rows = []inventory.Row{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

var csvHeader = []string{
	"namespace", "pod", "node", "container", "type", "image", "registry", "repository",
	"tag", "digest", "mutability", "running_digest", "current_digest", "moved",
	"created", "age_days", "check_error",
}

// InventoryCSV writes rows as CSV with a header line and full digests.
func InventoryCSV(w io.Writer, rows []inventory.Row) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range rows {
		created, age := "", ""
		if r.Created != nil {
			created = r.Created.Format(time.RFC3339)
		}
		if r.AgeDays != nil {
			age = strconv.Itoa(*r.AgeDays)
		}
		rec := []string{
			r.Namespace, r.Pod, r.Node, r.Container, r.Type, r.Image, r.Registry, r.Repository,
			r.Tag, r.Digest, r.Mutability, r.RunningDigest, r.CurrentDigest, strconv.FormatBool(r.Moved),
			created, age, r.CheckError,
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// short trims "sha256:<64 hex>" to "sha256:<12 hex>".
func short(d string) string {
	const keep = len("sha256:") + 12
	if d == "" {
		return "-"
	}
	if len(d) > keep {
		return d[:keep]
	}
	return d
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
