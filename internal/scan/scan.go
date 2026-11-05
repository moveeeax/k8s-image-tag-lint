// Package scan walks Kubernetes manifests and reports containers whose image
// reference is not pinned, according to the imageref policy.
package scan

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/moveeeax/k8s-image-tag-lint/internal/imageref"
	yaml "go.yaml.in/yaml/v3"
)

// workloadKinds are the manifest kinds that own a pod template. Anything else
// (Service, ConfigMap, CRDs, …) is skipped without error.
var workloadKinds = map[string]bool{
	"Deployment":  true,
	"StatefulSet": true,
	"DaemonSet":   true,
	"ReplicaSet":  true,
	"Job":         true,
	"CronJob":     true,
}

// Finding is a single unpinned-image violation.
type Finding struct {
	File      string `json:"file"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Container string `json:"container"`
	Init      bool   `json:"init"`
	Image     string `json:"image"`
	Reason    string `json:"reason"`
}

// Options controls the lint policy.
type Options struct {
	RequireDigest bool
}

type container struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

type podSpec struct {
	Containers     []container `yaml:"containers"`
	InitContainers []container `yaml:"initContainers"`
}

// manifest captures the two pod-template shapes we care about: the common
// spec.template.spec (Deployment, StatefulSet, …) and the nested
// spec.jobTemplate.spec.template.spec used by CronJob.
type manifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
		Template struct {
			Spec podSpec `yaml:"spec"`
		} `yaml:"template"`
		JobTemplate struct {
			Spec struct {
				Template struct {
					Spec podSpec `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		} `yaml:"jobTemplate"`
	} `yaml:"spec"`
}

func (m manifest) pod() podSpec {
	if m.Kind == "CronJob" {
		return m.Spec.JobTemplate.Spec.Template.Spec
	}
	return m.Spec.Template.Spec
}

// Paths walks every path (files or directories, recursively) and returns all
// findings, sorted for deterministic output.
func Paths(paths []string, opt Options) ([]Finding, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && isYAML(path) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	var findings []Finding
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		fs, err := scanBytes(f, data, opt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		findings = append(findings, fs...)
	}

	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Container < b.Container
	})
	return findings, nil
}

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// scanBytes decodes a (possibly multi-document) manifest byte stream and
// collects findings from every workload document in it.
func scanBytes(file string, data []byte, opt Options) ([]Finding, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var out []Finding
	for {
		var m manifest
		err := dec.Decode(&m)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if !workloadKinds[m.Kind] {
			continue
		}
		spec := m.pod()
		out = append(out, check(file, m, spec.Containers, false, opt)...)
		out = append(out, check(file, m, spec.InitContainers, true, opt)...)
	}
	return out, nil
}

func check(file string, m manifest, cs []container, init bool, opt Options) []Finding {
	var out []Finding
	for _, c := range cs {
		if c.Image == "" {
			continue
		}
		ref := imageref.Parse(c.Image)
		bad, reason := ref.Violation(opt.RequireDigest)
		if !bad {
			continue
		}
		out = append(out, Finding{
			File:      file,
			Kind:      m.Kind,
			Name:      m.Metadata.Name,
			Namespace: m.Metadata.Namespace,
			Container: c.Name,
			Init:      init,
			Image:     c.Image,
			Reason:    reason,
		})
	}
	return out
}
