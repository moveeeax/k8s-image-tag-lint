package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/moveeeax/k8s-image-tag-lint/internal/inventory"
)

func sampleRows() []inventory.Row {
	age := 30
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return []inventory.Row{
		{Namespace: "shop", Pod: "checkout", Container: "checkout", Type: inventory.TypeContainer,
			Image: "acme/checkout:latest", Registry: "docker.io", Repository: "acme/checkout", Tag: "latest",
			Mutability: inventory.MutLatest, RunningDigest: "sha256:" + strings.Repeat("b", 64),
			CurrentDigest: "sha256:" + strings.Repeat("e", 64), Moved: true, Created: &created, AgeDays: &age},
		{Namespace: "shop", Pod: "checkout", Container: "migrate", Type: inventory.TypeInit,
			Image: "busybox", Registry: "docker.io", Repository: "library/busybox",
			Mutability: inventory.MutUntagged, CheckError: "resolve tag: denied"},
		{Namespace: "default", Pod: "web-0", Container: "web", Type: inventory.TypeEphemeral,
			Image: "ghcr.io/acme/web:1.2", Registry: "ghcr.io", Repository: "acme/web", Tag: "1.2",
			Mutability: inventory.MutTag},
	}
}

func TestInventoryTableColumnsAndRegistryCounts(t *testing.T) {
	var buf bytes.Buffer
	if err := InventoryTable(&buf, sampleRows(), false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"REGISTRY", "MUTABILITY", "shop/checkout", "(init) migrate", "(ephemeral) web",
		"sha256:bbbbbbbbbbbb\n", "docker.io: 2 container(s)", "ghcr.io: 1 container(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "MOVED") {
		t.Errorf("check columns printed without --check-digests\n%s", out)
	}
}

func TestInventoryTableCheckedColumns(t *testing.T) {
	var buf bytes.Buffer
	if err := InventoryTable(&buf, sampleRows(), true); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"MOVED", "YES", "error", "30d",
		"1 container(s) run a digest their tag no longer points to", "1 container(s) could not be checked"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
}

func TestInventoryJSONAlwaysArray(t *testing.T) {
	var buf bytes.Buffer
	if err := InventoryJSON(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty inventory should encode as [], got %s", buf.String())
	}
	buf.Reset()
	if err := InventoryJSON(&buf, sampleRows()); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got[0]["moved"] != true || got[0]["age_days"] != float64(30) || got[0]["running_digest"] == nil {
		t.Errorf("unexpected JSON row: %v", got[0])
	}
}

func TestInventoryCSVFullDigests(t *testing.T) {
	var buf bytes.Buffer
	if err := InventoryCSV(&buf, sampleRows()); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Fatalf("expected header + 3 rows, got %d", len(recs))
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[h] = i
	}
	r := recs[1]
	if r[col["running_digest"]] != "sha256:"+strings.Repeat("b", 64) || r[col["moved"]] != "true" ||
		r[col["created"]] != "2026-10-01T00:00:00Z" || r[col["age_days"]] != "30" {
		t.Errorf("unexpected CSV row: %v", r)
	}
	if recs[2][col["check_error"]] != "resolve tag: denied" {
		t.Errorf("check error missing from CSV: %v", recs[2])
	}
}
