// Package imageref parses container image references and decides whether a
// reference is "pinned" enough to be safe, or whether it relies on a mutable
// tag that could resolve to a different digest on the next pull.
package imageref

import "strings"

// Ref is a parsed container image reference, split into its name, tag and
// digest components. The registry host (which may include a port such as
// localhost:5000) stays part of Name.
type Ref struct {
	Raw    string
	Name   string // registry + repository, e.g. "docker.io/library/nginx"
	Tag    string // tag without the leading colon, empty when absent
	Digest string // digest without the leading @, e.g. "sha256:abc…", empty when absent
}

// Parse splits an image reference into name, tag and digest. Digest is removed
// first (it contains a colon of its own), so the tag lookup only ever inspects
// the final path component and never confuses a registry port for a tag.
func Parse(image string) Ref {
	r := Ref{Raw: image}
	rest := image
	if i := strings.Index(rest, "@"); i >= 0 {
		r.Digest = rest[i+1:]
		rest = rest[:i]
	}
	// A tag colon can only live in the last path component; a colon before the
	// final slash belongs to a registry port.
	slash := strings.LastIndex(rest, "/")
	last := rest[slash+1:]
	if i := strings.Index(last, ":"); i >= 0 {
		r.Tag = last[i+1:]
		r.Name = rest[:slash+1] + last[:i]
	} else {
		r.Name = rest
	}
	return r
}

// Pinned reports whether the reference is immutable: an image is pinned when it
// carries a @sha256 digest, since the digest wins regardless of any tag.
func (r Ref) Pinned() bool { return r.Digest != "" }

// Violation classifies a reference against the lint policy. When requireDigest
// is true, anything without a digest is a violation. Otherwise only the two
// classic mutable-tag mistakes are flagged: a bare image with no tag, and an
// explicit :latest. A non-empty reason is returned only when ok is false.
func (r Ref) Violation(requireDigest bool) (bad bool, reason string) {
	if requireDigest {
		if r.Pinned() {
			return false, ""
		}
		return true, "image is not pinned to a @sha256 digest"
	}
	if r.Pinned() {
		return false, ""
	}
	if r.Tag == "" {
		return true, "image has no tag (implicitly :latest)"
	}
	if r.Tag == "latest" {
		return true, "image uses the mutable :latest tag"
	}
	return false, ""
}
