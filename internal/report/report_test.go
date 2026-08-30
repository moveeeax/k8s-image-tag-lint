package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moveeeax/k8s-image-tag-lint/internal/scan"
)

func TestJSONAlwaysArray(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, nil); err != nil {
		t.Fatal(err)
	}
	var got []scan.Finding
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output was not a JSON array: %v\n%s", err, buf.String())
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty findings should encode as [], got %s", buf.String())
	}
}

func TestTableContainsColumnsAndCount(t *testing.T) {
	findings := []scan.Finding{
		{File: "d.yaml", Kind: "Deployment", Name: "api", Namespace: "default", Container: "api", Image: "acme/api:latest", Reason: "mutable"},
		{File: "d.yaml", Kind: "CronJob", Name: "job", Container: "run", Init: true, Image: "busybox", Reason: "no tag"},
	}
	var buf bytes.Buffer
	if err := Table(&buf, findings); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"FILE", "REASON", "default/api", "acme/api:latest", "(init) run", "2 unpinned image(s) found"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
}

func TestTableAllClear(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no unpinned images found") {
		t.Errorf("expected all-clear line, got %q", buf.String())
	}
}
