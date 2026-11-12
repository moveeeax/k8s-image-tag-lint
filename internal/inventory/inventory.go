// Package inventory lists every image a set of running pods uses, as reported
// by `kubectl get pods -o json`. Each row splits the reference into registry,
// repository, tag and digest, and records the digest the kubelet actually
// resolved, so a floating tag that moved under a running pod can be detected
// by comparing that digest with what the tag resolves to today.
package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/moveeeax/k8s-image-tag-lint/internal/imageref"
)

// Container types, as they appear in the pod spec.
const (
	TypeContainer = "container"
	TypeInit      = "init"
	TypeEphemeral = "ephemeral"
)

// Mutability classes of the image reference written in the pod spec.
const (
	MutPinned   = "pinned"   // carries a @sha256 digest
	MutLatest   = "latest"   // explicit :latest
	MutUntagged = "untagged" // no tag, implicitly :latest
	MutTag      = "tag"      // some other tag, without a digest
)

// Row is one container of one pod.
type Row struct {
	Namespace  string `json:"namespace"`
	Pod        string `json:"pod"`
	Node       string `json:"node,omitempty"`
	Container  string `json:"container"`
	Type       string `json:"type"`
	Image      string `json:"image"`
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Tag        string `json:"tag,omitempty"`
	Digest     string `json:"digest,omitempty"`
	Mutability string `json:"mutability"`
	// RunningDigest is the repo digest from the container status imageID, empty
	// when the container has not started or the runtime reports only a local
	// image ID.
	RunningDigest string `json:"running_digest,omitempty"`

	// Filled by Check.
	CurrentDigest string     `json:"current_digest,omitempty"`
	Moved         bool       `json:"moved"`
	Created       *time.Time `json:"created,omitempty"`
	AgeDays       *int       `json:"age_days,omitempty"`
	CheckError    string     `json:"check_error,omitempty"`
}

// TagRef is the reference to resolve to find the digest the tag points at
// today, with an untagged image resolved as :latest.
func (r Row) TagRef() string {
	tag := r.Tag
	if tag == "" {
		tag = "latest"
	}
	return r.Registry + "/" + r.Repository + ":" + tag
}

// DigestRef is the reference of the image that actually runs: the resolved
// digest when known, else the digest from the spec, else empty.
func (r Row) DigestRef() string {
	d := r.RunningDigest
	if d == "" {
		d = r.Digest
	}
	if d == "" {
		return ""
	}
	return r.Registry + "/" + r.Repository + "@" + d
}

type containerSpec struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type containerStatus struct {
	Name    string `json:"name"`
	ImageID string `json:"imageID"`
}

type pod struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		NodeName            string          `json:"nodeName"`
		Containers          []containerSpec `json:"containers"`
		InitContainers      []containerSpec `json:"initContainers"`
		EphemeralContainers []containerSpec `json:"ephemeralContainers"`
	} `json:"spec"`
	Status struct {
		ContainerStatuses          []containerStatus `json:"containerStatuses"`
		InitContainerStatuses      []containerStatus `json:"initContainerStatuses"`
		EphemeralContainerStatuses []containerStatus `json:"ephemeralContainerStatuses"`
	} `json:"status"`
}

// document is either a single Pod or a List of them (kubectl prints a List for
// `get pods -o json` and a bare Pod for `get pod NAME -o json`).
type document struct {
	pod
	Items []pod `json:"items"`
}

// Read decodes pod JSON from r and returns one row per container, sorted by
// registry, then namespace, pod and container, so images from one registry
// group together.
func Read(r io.Reader) ([]Row, error) {
	var doc document
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode pod JSON: %w", err)
	}
	var pods []pod
	switch {
	case doc.Kind == "Pod":
		pods = []pod{doc.pod}
	case strings.HasSuffix(doc.Kind, "List"):
		pods = doc.Items
	default:
		return nil, fmt.Errorf("expected a Pod or a List of pods, got kind %q", doc.Kind)
	}

	var rows []Row
	for _, p := range pods {
		if p.Kind != "" && p.Kind != "Pod" {
			continue
		}
		rows = append(rows, podRows(p, p.Spec.Containers, p.Status.ContainerStatuses, TypeContainer)...)
		rows = append(rows, podRows(p, p.Spec.InitContainers, p.Status.InitContainerStatuses, TypeInit)...)
		rows = append(rows, podRows(p, p.Spec.EphemeralContainers, p.Status.EphemeralContainerStatuses, TypeEphemeral)...)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Registry != b.Registry {
			return a.Registry < b.Registry
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Pod != b.Pod {
			return a.Pod < b.Pod
		}
		return a.Container < b.Container
	})
	return rows, nil
}

func podRows(p pod, cs []containerSpec, statuses []containerStatus, typ string) []Row {
	imageIDs := map[string]string{}
	for _, s := range statuses {
		imageIDs[s.Name] = s.ImageID
	}
	var out []Row
	for _, c := range cs {
		if c.Image == "" {
			continue
		}
		ref := imageref.Parse(c.Image)
		registry, repo := splitName(ref.Name)
		out = append(out, Row{
			Namespace:     p.Metadata.Namespace,
			Pod:           p.Metadata.Name,
			Node:          p.Spec.NodeName,
			Container:     c.Name,
			Type:          typ,
			Image:         c.Image,
			Registry:      registry,
			Repository:    repo,
			Tag:           ref.Tag,
			Digest:        ref.Digest,
			Mutability:    mutability(ref),
			RunningDigest: repoDigest(imageIDs[c.Name]),
		})
	}
	return out
}

// splitName normalizes an image name the way the container runtime does
// (nginx → docker.io/library/nginx). A name that does not parse is kept whole
// as the repository with an empty registry.
func splitName(n string) (registry, repo string) {
	r, err := name.NewRepository(n)
	if err != nil {
		return "", n
	}
	registry = r.RegistryStr()
	if registry == name.DefaultRegistry {
		registry = "docker.io"
	}
	return registry, r.RepositoryStr()
}

func mutability(r imageref.Ref) string {
	switch {
	case r.Pinned():
		return MutPinned
	case r.Tag == "":
		return MutUntagged
	case r.Tag == "latest":
		return MutLatest
	default:
		return MutTag
	}
}

// repoDigest extracts the registry digest from a container status imageID.
// containerd and CRI-O report "docker.io/library/nginx@sha256:…", dockershim
// reported "docker-pullable://nginx@sha256:…". A bare "sha256:…" or
// "docker://sha256:…" is a local image ID, not a registry digest, and cannot be
// compared with what a tag resolves to, so it is ignored.
func repoDigest(imageID string) string {
	i := strings.LastIndex(imageID, "@")
	if i < 0 {
		return ""
	}
	d := imageID[i+1:]
	if !strings.HasPrefix(d, "sha256:") {
		return ""
	}
	return d
}

// Resolver looks images up in their registry.
type Resolver interface {
	// TagDigest returns the digest a tag reference resolves to today.
	TagDigest(ctx context.Context, ref string) (string, error)
	// Created returns the build time recorded in the image config.
	Created(ctx context.Context, ref string) (time.Time, error)
}

// Check fills the registry columns of every row: the digest the tag resolves
// to today (skipped for images pinned by digest in the spec, whose tag cannot
// move under them), whether it differs from the running digest, and the image
// build age. Each distinct reference is looked up once. A failed lookup is
// recorded in the row's CheckError and does not stop the others.
func Check(ctx context.Context, rows []Row, res Resolver, now time.Time) {
	type digestResult struct {
		digest string
		err    error
	}
	type createdResult struct {
		t   time.Time
		err error
	}
	digests := map[string]digestResult{}
	created := map[string]createdResult{}

	for i := range rows {
		r := &rows[i]
		if r.Registry == "" {
			r.CheckError = "cannot parse image name"
			continue
		}
		var errs []string

		if r.Mutability != MutPinned {
			ref := r.TagRef()
			got, ok := digests[ref]
			if !ok {
				d, err := res.TagDigest(ctx, ref)
				got = digestResult{d, err}
				digests[ref] = got
			}
			if got.err != nil {
				errs = append(errs, "resolve tag: "+got.err.Error())
			} else {
				r.CurrentDigest = got.digest
				r.Moved = r.RunningDigest != "" && r.RunningDigest != got.digest
			}
		}

		ref := r.DigestRef()
		if ref == "" {
			ref = r.TagRef()
		}
		got, ok := created[ref]
		if !ok {
			t, err := res.Created(ctx, ref)
			got = createdResult{t, err}
			created[ref] = got
		}
		if got.err != nil {
			errs = append(errs, "read config: "+got.err.Error())
		} else if !got.t.IsZero() {
			t := got.t.UTC()
			days := int(now.Sub(t).Hours() / 24)
			r.Created = &t
			r.AgeDays = &days
		}

		r.CheckError = strings.Join(errs, "; ")
	}
}
