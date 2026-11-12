# Releasing

The release is automated: pushing a `v*` tag runs `.github/workflows/release.yml`, which builds every platform with
goreleaser (`.goreleaser.yml`), signs the SHA256 checksums with GPG and creates a **draft** GitHub release.
`.github/workflows/ci.yml` checks pull requests and `main` (gofmt, vet, tests, `plugin-check`, generated files up to date).

Prerequisites (one time):

1. Public repository `agora-calyce/packer-plugin-superphenix` (the module path, `source` and `.web-docs/metadata.hcl` say so).
2. Repository secrets `GPG_PRIVATE_KEY` (armored private key) and `GPG_PASSPHRASE`. Packer only installs signed releases.
3. Integration registered on <https://developer.hashicorp.com/packer/integrations> with the matching GPG **public** key.

Each release:

1. `make generate && make test && make plugin-check`, then commit (the integrations library reads `.web-docs/` at the tag).
2. Date the `CHANGELOG.md` section of the version.
3. `git tag vX.Y.Z && git push origin vX.Y.Z`.
4. Review the draft release assets (zips, `_SHA256SUMS`, `.sig`), then publish it.
5. Check from a clean directory: `packer init` on a template that requires `>= X.Y.Z`.

The version comes from the tag: `Version` / `VersionPrerelease` in `main.go` are overridden at build time
(`VersionPrerelease` is emptied, otherwise `packer init` rejects a `>= X.Y.Z` constraint).

Dry run without publishing or signing (goreleaser v2 and `packer-sdc` installed):

```sh
API_VERSION=$(go run . describe | jq -r .api_version) GPG_FINGERPRINT=none \
  goreleaser release --snapshot --clean --skip=sign
```
