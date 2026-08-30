package scan

import (
	"path/filepath"
	"testing"
)

func fixture(name string) string { return filepath.Join("..", "..", "testdata", name) }

func TestCleanManifestExitsClean(t *testing.T) {
	f, err := Paths([]string{fixture("clean.yaml")}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 0 {
		t.Fatalf("expected no findings for clean manifest, got %d: %+v", len(f), f)
	}
}

func TestDirtyManifestFlagsMutableTags(t *testing.T) {
	f, err := Paths([]string{fixture("dirty.yaml")}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// api:latest, busybox (no tag), and the CronJob's acme/report (no tag).
	if len(f) != 3 {
		t.Fatalf("expected 3 findings, got %d: %+v", len(f), f)
	}
	byImage := map[string]Finding{}
	for _, v := range f {
		byImage[v.Image] = v
	}
	if _, ok := byImage["ghcr.io/acme/api:latest"]; !ok {
		t.Error("expected :latest image to be flagged")
	}
	if _, ok := byImage["acme/report"]; !ok {
		t.Error("expected CronJob untagged image to be flagged")
	}
	// The Service in the same directory scan must never appear.
	for _, v := range f {
		if v.Kind == "Service" {
			t.Errorf("non-workload kind leaked into findings: %+v", v)
		}
	}
}

func TestRequireDigestFlagsPlainTags(t *testing.T) {
	def, _ := Paths([]string{fixture("digest.yaml")}, Options{})
	if len(def) != 0 {
		t.Fatalf("postgres:16.3 should pass in default mode, got %+v", def)
	}
	strict, _ := Paths([]string{fixture("digest.yaml")}, Options{RequireDigest: true})
	if len(strict) != 1 {
		t.Fatalf("postgres:16.3 should fail --require digest, got %d", len(strict))
	}
}

func TestDirectoryWalkIsSortedAndDeterministic(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata")
	a, err := Paths([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Paths([]string{dir}, Options{})
	if len(a) != len(b) {
		t.Fatalf("non-deterministic count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic order at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
	if len(a) == 0 {
		t.Fatal("expected findings from the fixture directory")
	}
}

func TestMissingPathErrors(t *testing.T) {
	if _, err := Paths([]string{"does-not-exist.yaml"}, Options{}); err == nil {
		t.Fatal("expected error for missing path")
	}
}
