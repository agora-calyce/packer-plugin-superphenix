# Using the Superphenix Packer plugin (guide for AI agents)

Copy this file into the consuming project (for example as a section of its `AGENTS.md` or `CLAUDE.md`).
Plugin source: `github.com/agora-calyce/superphenix`. Full reference: `docs/builders/superphenix.mdx`,
`docs/post-processors/export.mdx`, working template: `example/ubuntu.pkr.hcl`, CI job: `example/.gitlab-ci.yml`.

## What it does

| Component | Block | Result |
|---|---|---|
| Builder | `source "superphenix" "<name>"` | Creates a VM from a **cloud image URL**, provisions it over **SSH**, stops it. |
| Post-processor | `post-processor "superphenix-export"` | Converts the VM disk to **qcow2** through a temporary helper VM and downloads it to `output_path` on the machine running Packer. |

The post-processor needs the builder artifact; it cannot be used with another builder.

## Hard constraints (check these before writing a template)

1. **Packer must run inside the Superphenix subnet (or behind a VPN).** It SSHes to the *private* IP of the
   build VM and of the helper. No EIP support. Outside the network the build hangs ~10 min then fails with
   `ssh to helper ...`.
2. **Exactly one source**: `source_url` (http(s) URL of a cloud image `.img`/`.qcow2`) **or** `iso_url` (http(s)
   URL of an installer ISO; Superphenix downloads it, so no local path and no `iso_checksum`). ISO installs
   are only tested with Ubuntu Server 26.04 (see "ISO installs" below).
3. **SSH only.** No WinRM, so no Windows yet. No `cd_files`/`cd_content`/`floppy_*` yet.
4. **Sizes are whole GiB strings**: `memory = "4Gi"`, `disk_size = "20Gi"`. No `4096`, `4G`, `512Mi`, `1.5Gi`.
5. The token is a secret: pass it with `variable "token" { sensitive = true }` fed by `PKR_VAR_token`
   (masked CI variable). Never write it in a template, a log, a commit or a message.
6. One build at a time per project (use GitLab `resource_group`).

## Required arguments

Builder: `api_url`, `org_id`, `az`, `project_id`, `token`, `subnet_eid`, `storage_class`, and one of `source_url` / `iso_url`.
Optional: `cpu` (2), `memory` ("4Gi"), `disk_size` ("20Gi"), `vm_type`, `user_data`, ISO only: `iso_disk_size` ("8Gi"), `efi` (true), `secure_boot`, `tpm`, `boot_command`, `boot_wait`, `http_content`/`http_directory`, plus the usual `communicator = "ssh"`
and `ssh_*` settings (`ssh_username` defaults to `spx`; the plugin creates that user itself with passwordless sudo).

Post-processor: `api_url`, `org_id`, `az`, `project_id`, `token`, `subnet_eid`, `storage_class`, `output_path`,
`helper_image_url` (any cloud-init Linux cloud image that can install `qemu-utils`; reusing the build image works).
Optional: `helper_cpu` (2), `helper_memory` ("2Gi"), `helper_disk_size` ("30Gi", must exceed the produced image),
`helper_ssh_username` (`spx`), `disk_device` (auto-detected; set only if detection fails).

Repeat the connection arguments in both blocks (`locals`/variables avoid duplication).

## Minimal template

```hcl
packer {
  required_plugins {
    superphenix = { source = "github.com/agora-calyce/superphenix", version = ">= 0.1.0" }
  }
}

variable "api_url"    { type = string }
variable "org_id"     { type = string }
variable "az"         { type = string }
variable "project_id" { type = string }
variable "subnet_eid" { type = string }
variable "token" {
  type      = string
  sensitive = true
}

locals { image = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img" }

source "superphenix" "ubuntu" {
  api_url       = var.api_url
  org_id        = var.org_id
  az            = var.az
  project_id    = var.project_id
  token         = var.token
  source_url    = local.image
  subnet_eid    = var.subnet_eid
  storage_class = "block-standard"
  cpu           = 2
  memory        = "4Gi"
  disk_size     = "20Gi"
  communicator = "ssh"
  ssh_username = "spx"
}

build {
  sources = ["source.superphenix.ubuntu"]
  provisioner "shell" { inline = ["sudo apt-get update"] }
  post-processor "superphenix-export" {
    api_url          = var.api_url
    org_id           = var.org_id
    az               = var.az
    project_id       = var.project_id
    token            = var.token
    subnet_eid       = var.subnet_eid
    storage_class    = "block-standard"
    helper_image_url = local.image
    output_path      = "output/ubuntu.qcow2"
  }
}
```

## Migrating from the `qemu` builder

| `qemu` | `superphenix` |
|---|---|
| `iso_url` (installer ISO) | `iso_url` (http(s) only) |
| `iso_url` + `disk_image = true` (cloud image) | `source_url` |
| `iso_checksum`, `iso_target_extension` | none (drop) |
| `cpus`, `memory` (MB int) | `cpu`, `memory = "<n>Gi"` |
| `disk_size` | `disk_size = "<n>Gi"` |
| `output_directory`, `vm_name`, `format = "qcow2"` | `post-processor "superphenix-export"` with `output_path = "<dir>/<name>.qcow2"` |
| `accelerator`, `headless`, `display`, `qemuargs`, `net_device`, `disk_interface` | drop (no equivalent) |
| `http_content`, `http_directory`, `boot_wait` | keep (VM must reach the Packer machine; prefer `user_data`) |
| `boot_command` | keep, typed over VNC. GRUB UEFI differs from BIOS: see "ISO installs" |
| `efi_boot`, `efi_firmware_*`, `qemuargs` | drop; `efi` (default true for ISO), `secure_boot`, `tpm` |
| `cd_files`, `floppy_*` (Windows) | not supported yet |
| `shutdown_command` | drop (the plugin stops the VM through the API) |
| `ssh_*` | keep `communicator = "ssh"` and `ssh_username`; password auth is not set up by the plugin, use the generated key |
| none | add `subnet_eid`, `storage_class`, `api_url`, `org_id`, `az`, `project_id`, `token` |

Provisioners (`shell`, `file`, `ansible`...) and other post-processors work unchanged. Update CI: replace
the QEMU/KVM runner requirement by a runner **inside the subnet**, export `PKR_VAR_*` variables, keep
`output_path` in the job `artifacts`.

## Customizing with cloud-init (`user_data`)

`user_data = file("user-data.yml")` sets cloud-init on the build VM's instance (the "CloudInit configuration" of the
console). Only `#cloud-config` YAML is accepted (no shell script or MIME), checked at `packer validate`. It never
applies to the export helper. The behaviour depends on the source:

| Source | What happens to `user_data` |
|---|---|
| `source_url` (cloud image) | **Merged** with the plugin's cloud-init: `users` and `packages` lists are appended (the SSH user, with passwordless sudo and the temporary key, is always created); every other key of yours wins. |
| `iso_url` (installer) | **Passed as is**, no merge and no SSH user added. The installer reads it from the VM's cloud-init disk (for Ubuntu, an `autoinstall:` section). Your config must create the user Packer connects with and enable SSH. |

Prefer `provisioner` blocks for anything that must run after SSH is up. `user_data` is for first-boot cloud-init
(cloud image) or the installer's answer file (ISO).

## ISO installs

`example/ubuntu-iso.pkr.hcl` is a tested template (Ubuntu Server 26.04). The mechanism:

- Disks: empty `disk_size` disk first, ISO as CD-ROM second, **UEFI** firmware (BIOS never boots the CD-ROM).
  The installed disk boots by itself after the installer reboots; the ISO disk is deleted at the end.
- Answer file: put it in `user_data` (`#cloud-config` with an `autoinstall:` section). It is set on the instance's
  cloud-init, read by the installer without any network, and passed as is. Prefer it over `http_content`, which
  needs the VM to reach the Packer machine (a security group may block it).
- `boot_command` keeps `autoinstall` on the kernel command line, inserted **before** the `---` of the `linux`
  line: `e<wait><down><down><down><end><left><left><left><left>` then `autoinstall `, then `<f10>`. It depends on
  the ISO: when changing release, check the GRUB editor on a VNC screenshot instead of guessing.
- The autoinstall must create the SSH user Packer uses and enable SSH (with `allow-pw` for password auth).
- The post-processor has not been re-tested on a VM built from an ISO. The qcow2 holds an EFI system: boot it with EFI.

## Validate and run

```sh
packer init <dir>
packer fmt -check <dir>
packer validate <dir>       # needs all variables; catches missing/unknown arguments and bad sizes
packer build -color=false <dir>
```

Typical durations: image import a few minutes, then provisioning, then conversion + download. Set the CI job
`timeout` generously (example uses 1h).

## Troubleshooting

| Symptom | Cause / action |
|---|---|
| `exactly one of source_url and iso_url is required` | Set only one source. |
| ISO build stuck at "No bootable device" | Firmware is BIOS: leave `efi` unset (default true) for `iso_url`. |
| Installer asks questions / ignores the answer file | `autoinstall` missing from the kernel line, or typed after `---`; check the GRUB screen. |
| `Unsupported argument "eip_eid"` / `"runner_cidr"` | Removed options. Delete them; run Packer inside the subnet. |
| `memory: ...` / `disk_size: ...` error | Use whole GiB strings like `"4Gi"`. |
| `<arg> is required` | Fill the listed argument (both blocks need their own connection arguments). |
| `ssh to helper <ip>: ...` or build SSH timeout | Runner cannot reach the private IP. Check it is in the subnet / VPN and the subnet allows SSH. |
| Helper never gets `qemu-img` | The subnet needs internet access to the distro package mirrors. |
| `401`/`403` from the API | Token wrong, expired, or lacks rights on the project. Do not print it while debugging. |
| Export disk not found | Set `disk_device` (for example `/dev/vdb`). |
| Output file empty/truncated, no space | Increase `helper_disk_size` (it holds the converted qcow2). |

Failed or cancelled runs delete their resources. If Packer was killed (SIGKILL, runner crash), orphans may
remain: look for VMs/disks labelled `user.superphenix.net/created-by:packer*` (builder: `packer`, helper:
`packer-helper-<timestamp>`) and delete them **only after confirming with the user**.

## Rules for agents

- Never print, log or commit the token or any `PKR_VAR_token` value.
- Do not invent arguments: only those listed above exist; check `docs/` when unsure.
- Creating, deleting or listing real Superphenix resources (API calls, orphan cleanup) needs the user's go-ahead.
- Prefer `packer validate` before `packer build`; a build creates billable resources.
