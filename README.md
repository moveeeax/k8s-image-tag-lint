# k8s-image-tag-lint

[![ci](https://github.com/moveeeax/k8s-image-tag-lint/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/k8s-image-tag-lint/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Flag Kubernetes workloads that use a **mutable image tag** — `:latest`, no tag at
all, or (in strict mode) any tag without a `@sha256` digest — and fail CI before
an unpinned image ever reaches a cluster.

A workload pinned to `:latest` is a silent supply-chain and rollback hazard: two
nodes can pull two different images under one tag, and a postmortem can't say
which image actually ran. `k8s-image-tag-lint` is a cheap gate that rejects that
at pull-request time.

## What it does

- Walks files and directories (recursively) for Kubernetes YAML, parsing
  multi-document manifests.
- Inspects every container and initContainer of `Deployment`, `StatefulSet`,
  `DaemonSet`, `ReplicaSet`, `Job` and `CronJob`. Non-workload kinds are skipped.
- Flags an image when its tag is `:latest`, when it has no tag at all, or — with
  `--require digest` — when it lacks a `@sha256` digest.
- Prints an aligned table or, with `--json`, a machine-readable findings array.
- Exits non-zero when anything is flagged, so it gates CI.

## How it works

Each manifest document is decoded into the two pod-template shapes that matter:
the common `spec.template.spec` and the nested `spec.jobTemplate.spec.template.spec`
used by `CronJob`. Each image string is parsed by splitting off the `@sha256`
digest first, then reading the tag from the final path component only — so a
registry port like `localhost:5000/app` is never mistaken for a tag. A digest
always counts as pinned; the rest is policy.

## Install

```shell
go install github.com/moveeeax/k8s-image-tag-lint@latest
```

Or build from source:

```shell
git clone https://github.com/moveeeax/k8s-image-tag-lint.git
cd k8s-image-tag-lint
go build -o k8s-image-tag-lint .
```

## Usage

```shell
# default: flag :latest and untagged images
k8s-image-tag-lint scan ./manifests

# strict: require a @sha256 digest, machine-readable output
k8s-image-tag-lint scan --require digest --json deploy.yaml
```

Example against `examples/manifests`:

```text
FILE                                  KIND        NAME            CONTAINER  IMAGE                          REASON
examples/manifests/deployment.yaml    Deployment  shop/checkout   checkout   ghcr.io/acme/checkout:latest   image uses the mutable :latest tag
examples/manifests/deployment.yaml    Deployment  shop/checkout   envoy      envoyproxy/envoy               image has no tag (implicitly :latest)

2 unpinned image(s) found
```

The digest-pinned `report` container and the tagged `busybox:1.36` initContainer
are correctly left alone.

## In CI

```yaml
- name: Lint image tags
  run: |
    go install github.com/moveeeax/k8s-image-tag-lint@latest
    k8s-image-tag-lint scan ./k8s
```

The non-zero exit on any finding fails the job.

## License

MIT — see [LICENSE](LICENSE).
