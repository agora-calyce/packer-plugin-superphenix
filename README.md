# Packer Plugin Superphenix

A [Packer](https://www.packer.io) plugin that builds machine images on a
[Superphenix](https://github.com/super-phenix/superphenix) hypervisor and exports them as `qcow2` files, built
to run in CI (for example GitLab CI).

> **Status: early development.** The builder and the export post-processor have been run end to end against a
> Superphenix environment (Ubuntu cloud image, runner inside the subnet). Other images, AZs and API versions are
> untested (see [DESIGN.md](DESIGN.md) and [Known limitations](#known-limitations)).

## Components

| Component | Name | Description |
|---|---|---|
| Builder | `superphenix` | Creates a VM from a cloud image, provisions it over SSH, stops it. |
| Post-processor | `superphenix-export` | Converts the VM disk to `qcow2` through a short-lived helper VM and downloads it. |

Superphenix has no disk export endpoint, so the post-processor detaches the disk, attaches it to a helper VM
that has `qemu-img`, converts it there, and fetches the file over SSH. Everything is cleaned up even when a step
fails or the build is cancelled. Details: [DESIGN.md](DESIGN.md).

## Installation

```hcl
packer {
  required_plugins {
    superphenix = {
      version = ">= 0.1.0"
      source  = "github.com/agora-calyce/superphenix"
    }
  }
}
```

Then run `packer init`.

## Usage

A complete template is in [`example/ubuntu.pkr.hcl`](example/ubuntu.pkr.hcl), and a GitLab CI job in
[`example/.gitlab-ci.yml`](example/.gitlab-ci.yml) (move it to the repository root to use it).

Pass the API token through a sensitive variable (`PKR_VAR_token`, a masked CI variable), never in a template.

Reference documentation:

- [Builder](docs/builders/superphenix.mdx)
- [Post-processor](docs/post-processors/export.mdx)

## Requirements

- A Superphenix API URL, organization, availability zone, project, and an API token (created from the console
  "API tokens" page, sent as `Authorization: api-token <token>`).
- A subnet, and a storage class (the available ones are listed by `GET .../{az}/{project_id}/storage-class`).
- The machine running Packer must reach the **private** IP of the VMs over SSH, for the builder and for the
  export helper: run Packer on a VM inside the subnet, or behind a VPN. Exposing the VMs through an EIP is not
  supported.

## Known limitations

- Cloud images only (`http` disk source); no ISO installation. Customize with `user_data` (`#cloud-config`) and provisioners.
- SSH only; no WinRM, so no Windows images yet.
- The helper's SSH host key is not verified (the helper is ephemeral).
- The runner must be inside the Superphenix network (see Requirements).
- Sizes follow the deployed API: `memory` is sent as a whole number of GiB (`4Gi`), and so is `disk_size`.
  Newer API versions accept a quantity string for `memory`; this will need to follow when the API is upgraded.
- Tested with one API version and one AZ (`fr01-als01`).

## Development

The project directory must be named `packer-plugin-*` for `packer-sdc` to generate docs.

```sh
make build        # build the plugin binary
make test         # unit tests
make generate     # regenerate hcl2spec files, doc partials and .web-docs/ (requires packer-sdc)
make plugin-check # validate the binary with packer-sdc
```

Install `packer-sdc` with `go install github.com/hashicorp/packer-plugin-sdk/cmd/packer-sdc@latest`. To try a
local build: `packer plugins install --path packer-plugin-superphenix github.com/agora-calyce/superphenix`.

Docs are written in `docs/` and rendered to `.web-docs/` by `make generate`; commit the result, it is what the
Packer integrations library reads (`.web-docs/metadata.hcl` describes the integration).

## License

[MPL-2.0](LICENSE)
