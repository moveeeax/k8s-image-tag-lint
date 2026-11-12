// Package registry resolves image references against their registry with
// go-containerregistry, using the local Docker credential config (and its
// credential helpers) for authentication and anonymous access otherwise.
package registry

import (
	"context"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Client implements inventory.Resolver.
type Client struct {
	// Platform picks the image from a multi-platform index when reading its
	// build time.
	Platform v1.Platform
	// Options are appended to every remote call (tests inject a transport).
	Options []remote.Option
}

// New returns a client that authenticates with the default keychain.
func New(platform v1.Platform) *Client {
	return &Client{
		Platform: platform,
		Options:  []remote.Option{remote.WithAuthFromKeychain(authn.DefaultKeychain)},
	}
}

func (c *Client) opts(ctx context.Context) []remote.Option {
	return append([]remote.Option{remote.WithContext(ctx), remote.WithPlatform(c.Platform)}, c.Options...)
}

// TagDigest returns the digest the reference resolves to. For a multi-platform
// image that is the index digest, the same one the kubelet records in the
// container status. HEAD is tried first because it does not count against
// Docker Hub pull limits; GET is the fallback for registries without HEAD.
func (c *Client) TagDigest(ctx context.Context, ref string) (string, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return "", err
	}
	if d, err := remote.Head(r, c.opts(ctx)...); err == nil {
		return d.Digest.String(), nil
	}
	d, err := remote.Get(r, c.opts(ctx)...)
	if err != nil {
		return "", err
	}
	return d.Digest.String(), nil
}

// Created returns the build time from the image config. An index is resolved
// to the image for c.Platform first.
func (c *Client) Created(ctx context.Context, ref string) (time.Time, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return time.Time{}, err
	}
	img, err := remote.Image(r, c.opts(ctx)...)
	if err != nil {
		return time.Time{}, err
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return time.Time{}, err
	}
	return cfg.Created.Time, nil
}
