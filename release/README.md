# Release files (prepared, not active)

These files publish the plugin to the Packer integrations library but are **not taken into account** while
they stay in this directory: GitHub Actions only reads `.github/workflows/`, and goreleaser only reads a
`.goreleaser.yml` at the repository root. Nothing here has been run (no goreleaser, no GitHub Actions in the
development environment), so expect to adjust them on the first release.

| File | Role once activated |
|---|---|
| `goreleaser.yml` | Builds the binaries (`<name>_v<version>_<api_version>_<os>_<arch>` zips), the SHA256 checksums file and its GPG signature. Release is created as a draft. |
| `workflows/release.yml` | On a `v*` tag: computes the plugin API version, imports the GPG key, runs goreleaser. |
| `workflows/ci.yml` | On pull requests and `main`: gofmt, vet, tests, `plugin-check`, and checks that `make generate` leaves no diff. |

## Before activating

1. **Repository**: public, named `packer-plugin-superphenix`, under the `agora-calyce` organization (the module
   path, `source` and `.web-docs/metadata.hcl` already say so).
2. **GPG key**: create a key for signing, then add two repository secrets: `GPG_PRIVATE_KEY` (the exported
   armored private key) and `GPG_PASSPHRASE`. Packer installs plugins only from signed releases; the public key
   is given to HashiCorp when you register the integration (below).
3. **Register the integration** on <https://developer.hashicorp.com/packer/integrations> (sign in with GitHub,
   choose the repository, and provide the public key). It reads `.web-docs/` at the tag: run `make generate`
   and commit the result first.
4. **Check the pinned actions** (`actions/checkout`, `actions/setup-go`, `crazy-max/ghaction-import-gpg`,
   `goreleaser/goreleaser-action`) and the platforms in `goreleaser.yml` (`goos` / `goarch`): trim them if you only
   need Linux.

## Activate

```sh
mkdir -p .github/workflows
git mv release/goreleaser.yml .goreleaser.yml
git mv release/workflows/release.yml .github/workflows/release.yml
git mv release/workflows/ci.yml .github/workflows/ci.yml   # optional
```

Then update `CHANGELOG.md`, commit, and push a tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

The workflow publishes a **draft** release: review the assets, then publish it. The version comes from the tag:
the `Version` / `VersionPrerelease` variables in `main.go` are overridden at build time (`VersionPrerelease`
is emptied, otherwise `packer init` rejects a `>= 0.1.0` constraint).

## Test locally first (optional)

With goreleaser v2 and `packer-sdc` installed:

```sh
API_VERSION=$(go run . describe | jq -r .api_version) GPG_FINGERPRINT=none \
  goreleaser release --snapshot --clean --skip=sign --config release/goreleaser.yml
```

This builds every platform into `dist/` without publishing or signing anything.
