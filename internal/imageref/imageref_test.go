package imageref

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in                string
		name, tag, digest string
	}{
		{"nginx", "nginx", "", ""},
		{"nginx:1.27", "nginx", "1.27", ""},
		{"nginx:latest", "nginx", "latest", ""},
		{"docker.io/library/nginx:1.27", "docker.io/library/nginx", "1.27", ""},
		{"localhost:5000/app:v2", "localhost:5000/app", "v2", ""},
		{"localhost:5000/app", "localhost:5000/app", "", ""},
		{"nginx@sha256:abc", "nginx", "", "sha256:abc"},
		{"nginx:1.27@sha256:abc", "nginx", "1.27", "sha256:abc"},
		{"registry.example.com:5000/team/app:1.0@sha256:def", "registry.example.com:5000/team/app", "1.0", "sha256:def"},
	}
	for _, c := range cases {
		got := Parse(c.in)
		if got.Name != c.name || got.Tag != c.tag || got.Digest != c.digest {
			t.Errorf("Parse(%q) = {name:%q tag:%q digest:%q}, want {name:%q tag:%q digest:%q}",
				c.in, got.Name, got.Tag, got.Digest, c.name, c.tag, c.digest)
		}
	}
}

func TestViolationDefault(t *testing.T) {
	cases := []struct {
		in  string
		bad bool
	}{
		{"nginx", true},                    // no tag
		{"nginx:latest", true},             // mutable
		{"nginx:1.27", false},              // pinned tag ok in default mode
		{"nginx@sha256:abc", false},        // digest pins
		{"nginx:latest@sha256:abc", false}, // digest wins over tag
		{"nginx:1.27@sha256:abc", false},
	}
	for _, c := range cases {
		bad, reason := Parse(c.in).Violation(false)
		if bad != c.bad {
			t.Errorf("Violation(%q, default) = %v, want %v", c.in, bad, c.bad)
		}
		if bad && reason == "" {
			t.Errorf("Violation(%q) flagged but gave no reason", c.in)
		}
	}
}

func TestViolationRequireDigest(t *testing.T) {
	cases := []struct {
		in  string
		bad bool
	}{
		{"nginx", true},
		{"nginx:latest", true},
		{"nginx:1.27", true}, // strict: a tag without a digest is not enough
		{"nginx@sha256:abc", false},
		{"nginx:1.27@sha256:abc", false},
	}
	for _, c := range cases {
		bad, _ := Parse(c.in).Violation(true)
		if bad != c.bad {
			t.Errorf("Violation(%q, requireDigest) = %v, want %v", c.in, bad, c.bad)
		}
	}
}
