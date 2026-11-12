package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureRows(t *testing.T) []Row {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "pods.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func byContainer(rows []Row) map[string]Row {
	m := map[string]Row{}
	for _, r := range rows {
		m[r.Container] = r
	}
	return m
}

func TestReadSplitsEveryContainerType(t *testing.T) {
	rows := fixtureRows(t)
	if len(rows) != 6 {
		t.Fatalf("expected 6 rows (init, 3 containers, ephemeral, pending), got %d: %+v", len(rows), rows)
	}
	m := byContainer(rows)

	cases := []struct {
		container, typ, registry, repo, tag, mut, running string
	}{
		{"migrate", TypeInit, "docker.io", "library/busybox", "1.36", MutTag, "sha256:" + strings.Repeat("a", 64)},
		{"checkout", TypeContainer, "ghcr.io", "acme/checkout", "latest", MutLatest, "sha256:" + strings.Repeat("b", 64)},
		{"envoy", TypeContainer, "docker.io", "envoyproxy/envoy", "", MutUntagged, "sha256:" + strings.Repeat("d", 64)},
		{"report", TypeContainer, "ghcr.io", "acme/report", "", MutPinned, "sha256:" + strings.Repeat("c", 64)},
		// A bare local image ID is not a registry digest.
		{"debugger", TypeEphemeral, "docker.io", "nicolaka/netshoot", "v0.13", MutTag, ""},
		// Pending pod: no status yet; registry port is not a tag.
		{"nginx", TypeContainer, "localhost:5000", "web", "1.2", MutTag, ""},
	}
	for _, c := range cases {
		r, ok := m[c.container]
		if !ok {
			t.Errorf("missing row for %s", c.container)
			continue
		}
		if r.Type != c.typ || r.Registry != c.registry || r.Repository != c.repo || r.Tag != c.tag || r.Mutability != c.mut || r.RunningDigest != c.running {
			t.Errorf("%s: got type=%s registry=%s repo=%s tag=%s mut=%s running=%s", c.container, r.Type, r.Registry, r.Repository, r.Tag, r.Mutability, r.RunningDigest)
		}
	}
	if m["checkout"].Node != "node-a" || m["checkout"].Namespace != "shop" || m["checkout"].Pod != "checkout-7d9f" {
		t.Errorf("pod metadata not copied: %+v", m["checkout"])
	}
}

func TestReadGroupsByRegistry(t *testing.T) {
	rows := fixtureRows(t)
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Registry > rows[i].Registry {
			t.Fatalf("rows not sorted by registry at %d: %s after %s", i, rows[i].Registry, rows[i-1].Registry)
		}
	}
}

func TestReadSinglePod(t *testing.T) {
	rows, err := Read(strings.NewReader(`{"kind":"Pod","metadata":{"name":"p","namespace":"n"},"spec":{"containers":[{"name":"c","image":"nginx"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Repository != "library/nginx" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestReadRejectsOtherKinds(t *testing.T) {
	if _, err := Read(strings.NewReader(`{"kind":"Deployment"}`)); err == nil {
		t.Fatal("expected error for a non-pod document")
	}
	if _, err := Read(strings.NewReader(`not json`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

type fakeResolver struct {
	tags    map[string]string
	created map[string]time.Time
	calls   map[string]int
}

func (f *fakeResolver) TagDigest(_ context.Context, ref string) (string, error) {
	f.calls["tag "+ref]++
	d, ok := f.tags[ref]
	if !ok {
		return "", errors.New("not found")
	}
	return d, nil
}

func (f *fakeResolver) Created(_ context.Context, ref string) (time.Time, error) {
	f.calls["cfg "+ref]++
	c, ok := f.created[ref]
	if !ok {
		return time.Time{}, errors.New("not found")
	}
	return c, nil
}

func TestCheckFlagsMovedTagsAndAge(t *testing.T) {
	rows := fixtureRows(t)
	sha := func(c string) string { return "sha256:" + strings.Repeat(c, 64) }
	now := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	res := &fakeResolver{
		tags: map[string]string{
			"docker.io/library/busybox:1.36":    sha("a"), // unchanged
			"ghcr.io/acme/checkout:latest":      sha("e"), // moved
			"docker.io/envoyproxy/envoy:latest": sha("d"), // untagged resolves as :latest
			"docker.io/nicolaka/netshoot:v0.13": sha("f"),
			"localhost:5000/web:1.2":            sha("1"),
		},
		created: map[string]time.Time{
			"ghcr.io/acme/checkout@" + sha("b"): now.Add(-30 * 24 * time.Hour),
			"ghcr.io/acme/report@" + sha("c"):   now.Add(-2 * 24 * time.Hour),
		},
		calls: map[string]int{},
	}
	Check(context.Background(), rows, res, now)
	m := byContainer(rows)

	if !m["checkout"].Moved || m["checkout"].CurrentDigest != sha("e") {
		t.Errorf("checkout:latest should be reported as moved: %+v", m["checkout"])
	}
	if m["migrate"].Moved || m["envoy"].Moved {
		t.Errorf("unchanged tags reported as moved: %+v %+v", m["migrate"], m["envoy"])
	}
	// Without a running digest there is nothing to compare.
	if m["debugger"].Moved || m["nginx"].Moved {
		t.Errorf("rows without running digest reported as moved")
	}
	if m["nginx"].CurrentDigest != sha("1") {
		t.Errorf("pending pod tag should still resolve: %+v", m["nginx"])
	}
	// Pinned in the spec: tag lookup skipped, age read from the digest.
	if res.calls["tag ghcr.io/acme/report:latest"] != 0 || m["report"].CurrentDigest != "" {
		t.Errorf("pinned image should not resolve a tag: %+v", m["report"])
	}
	if a := m["report"].AgeDays; a == nil || *a != 2 {
		t.Errorf("report age: got %v, want 2", a)
	}
	if a := m["checkout"].AgeDays; a == nil || *a != 30 {
		t.Errorf("checkout age should come from the running digest: got %v", a)
	}
	if !strings.Contains(m["migrate"].CheckError, "read config") || m["migrate"].CurrentDigest == "" {
		t.Errorf("config failure should be recorded without hiding the tag result: %+v", m["migrate"])
	}
}

func TestCheckLooksUpEachReferenceOnce(t *testing.T) {
	rows := []Row{
		{Pod: "a", Container: "c", Registry: "docker.io", Repository: "library/nginx", Tag: "1.27", Mutability: MutTag},
		{Pod: "b", Container: "c", Registry: "docker.io", Repository: "library/nginx", Tag: "1.27", Mutability: MutTag},
	}
	res := &fakeResolver{tags: map[string]string{}, created: map[string]time.Time{}, calls: map[string]int{}}
	Check(context.Background(), rows, res, time.Now())
	if n := res.calls["tag docker.io/library/nginx:1.27"]; n != 1 {
		t.Errorf("tag looked up %d times, want 1", n)
	}
	if rows[0].CheckError == "" || rows[1].CheckError == "" {
		t.Errorf("errors should be recorded on every row sharing the reference")
	}
}
