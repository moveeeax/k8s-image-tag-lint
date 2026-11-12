package registry

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// testRegistry starts an in-memory registry and returns its host:port.
func testRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func push(t *testing.T, ref string, created time.Time) v1.Image {
	t.Helper()
	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatal(err)
	}
	img, err = mutate.CreatedAt(img, v1.Time{Time: created})
	if err != nil {
		t.Fatal(err)
	}
	r, err := name.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(r, img); err != nil {
		t.Fatal(err)
	}
	return img
}

func TestTagDigestFollowsRetag(t *testing.T) {
	host := testRegistry(t)
	ref := host + "/acme/app:latest"
	c := &Client{Platform: v1.Platform{OS: "linux", Architecture: "amd64"}}
	ctx := context.Background()

	first := push(t, ref, time.Now())
	d1, err := c.TagDigest(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := first.Digest()
	if d1 != want.String() {
		t.Fatalf("got %s, want %s", d1, want)
	}

	push(t, ref, time.Now())
	d2, err := c.TagDigest(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if d2 == d1 {
		t.Fatal("digest should change after the tag is pushed again")
	}
}

func TestCreatedReadsConfig(t *testing.T) {
	host := testRegistry(t)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	img := push(t, host+"/acme/app:1.0", created)
	d, _ := img.Digest()
	c := &Client{Platform: v1.Platform{OS: "linux", Architecture: "amd64"}}

	got, err := c.Created(context.Background(), host+"/acme/app@"+d.String())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(created) {
		t.Fatalf("got %s, want %s", got, created)
	}
}

func TestMissingTagErrors(t *testing.T) {
	host := testRegistry(t)
	c := &Client{}
	if _, err := c.TagDigest(context.Background(), host+"/acme/none:1"); err == nil {
		t.Fatal("expected error for a tag that does not exist")
	}
}
