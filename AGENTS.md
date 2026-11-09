# AGENTS.md

Packer plugin (Go, `packer-plugin-sdk`) for the Superphenix hypervisor: builder `superphenix` and
post-processor `superphenix-export` (qcow2 export). Built for CI.

- **Using the plugin in a Packer project** (templates, QEMU migration, troubleshooting): read
  [`docs/agent-usage.md`](docs/agent-usage.md).
- **Changing the plugin**: read [`DESIGN.md`](DESIGN.md) (API endpoints, export flow, known quirks),
  then the rules below.

## Layout

| Path | Role |
|---|---|
| `main.go` | Plugin registration. |
| `builder/` | Builder: `builder.go` (config, `Prepare`, steps list), `steps.go` (create VM, stop). |
| `postprocessor/` | Export: `postprocessor.go` (helper VM flow, deferred cleanup), `transfer.go` (SSH conversion + resumable download). |
| `internal/client/` | Hand-written Superphenix HTTP client (no public Go SDK). |
| `docs/`, `docs-partials/` | Reference docs (`.mdx`) and generated partials. |
| `example/` | Reference template and GitLab CI job. |

## Commands

```sh
make build        # binary
make test         # unit tests (go test ./...)
go vet ./... && gofmt -l .
make generate     # regenerate *.hcl2spec.go, docs-partials, .web-docs (needs packer-sdc)
make plugin-check
```

## Rules

- After editing a `Config` struct or its field comments, run `make generate`; never hand-edit
  `*.hcl2spec.go` or `docs-partials/`.
- Config/behaviour changes: update `docs/`, `README.md`, `example/`, `docs/agent-usage.md` and
  `CHANGELOG.md` (flag breaking changes).
- Every resource created on Superphenix must be removed by a `defer`/`Cleanup` (success, failure, Ctrl-C)
  and carry a `user.superphenix.net/created-by:packer*` label.
- Superphenix API quirks are in `DESIGN.md` (sizes as whole GiB, disk attach only at VM creation, async VM
  deletion, wait for disk import `Succeeded`). Check them before touching `internal/client` or the flows.
- Network model: the runner reaches **private IPs** (no EIP). Do not reintroduce public exposure silently.
- Secrets: the API token is never logged, printed, or put in examples. Examples use variables.
- Everything is written in English: code comments, docs, tests, commit messages. Prefer the smallest change.
- Do not call the real Superphenix API, or commit/push, without the user's explicit go-ahead.
- Tests: unit tests mock the API (`transfer` is injectable in `PostProcessor`). End-to-end runs need a runner
  in the subnet and real credentials.
