package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moveeeax/k8s-image-tag-lint/internal/inventory"
)

type stubResolver struct{ digest string }

func (s stubResolver) TagDigest(context.Context, string) (string, error) { return s.digest, nil }
func (s stubResolver) Created(context.Context, string) (time.Time, error) {
	return time.Time{}, errors.New("no config")
}

func noRegistry(t *testing.T) func(v1.Platform) inventory.Resolver {
	return func(v1.Platform) inventory.Resolver {
		t.Fatal("registry used without --check-digests")
		return nil
	}
}

var podsFixture = filepath.Join("testdata", "pods.json")

func TestInventoryWithoutCheckExitsZero(t *testing.T) {
	var out bytes.Buffer
	if code := runInventory([]string{podsFixture}, nil, &out, noRegistry(t)); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(out.String(), "acme/checkout") || strings.Contains(out.String(), "MOVED") {
		t.Errorf("unexpected output\n%s", out.String())
	}
}

func TestInventoryReadsStdin(t *testing.T) {
	in := strings.NewReader(`{"kind":"Pod","metadata":{"name":"p","namespace":"n"},"spec":{"containers":[{"name":"c","image":"nginx"}]}}`)
	var out bytes.Buffer
	if code := runInventory([]string{"--output", "csv", "-"}, in, &out, noRegistry(t)); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(out.String(), "library/nginx") {
		t.Errorf("unexpected output\n%s", out.String())
	}
}

func TestInventoryMovedTagExitsOne(t *testing.T) {
	// Every tag resolves to a digest no container runs, so each container with
	// a known running digest and a mutable reference has moved.
	res := func(p v1.Platform) inventory.Resolver {
		if p.OS != "linux" || p.Architecture != "arm64" {
			t.Errorf("platform flag not passed: %+v", p)
		}
		return stubResolver{digest: "sha256:" + strings.Repeat("9", 64)}
	}
	var out bytes.Buffer
	code := runInventory([]string{"--check-digests", "--platform", "linux/arm64", "--output", "json", podsFixture}, nil, &out, res)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	var rows []inventory.Row
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	moved := 0
	for _, r := range rows {
		if r.Moved {
			moved++
		}
	}
	// migrate, checkout, envoy; report is pinned, debugger and nginx have no running digest.
	if moved != 3 {
		t.Errorf("moved = %d, want 3", moved)
	}
}

func TestInventoryUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--output", "yaml", podsFixture},
		{"--platform", "", podsFixture},
		{podsFixture, podsFixture},
		{"missing.json"},
	} {
		if code := runInventory(args, nil, &bytes.Buffer{}, noRegistry(t)); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
