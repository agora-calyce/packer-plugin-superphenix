# Using the Superphenix Packer plugin (guide for AI agents)

Copy this file into the consuming project (for example as a section of its `AGENTS.md` or `CLAUDE.md`).
Plugin source: `github.com/agora-calyce/superphenix`. Full reference: `docs/builders/superphenix.mdx`,
`docs/post-processors/export.mdx`. User guides: `docs/linux.md`, `docs/windows.md`. Working templates:
`example/ubuntu.pkr.hcl` (cloud image), `example/ubuntu-iso.pkr.hcl` (Linux ISO), `example/windows/` (Windows),
CI job: `example/.gitlab-ci.yml`.

## What it does

| Component | Block | Result |
|---|---|---|
| Builder | `source "superphenix" "<name>"` | Creates a VM from a **cloud image URL** or an **installer ISO** (Linux, Windows), provisions it over **SSH** or **WinRM**, stops it. |
| Post-processor | `post-processor "superphenix-export"` | Converts the VM disk to **qcow2** through a temporary helper VM and downloads it to `output_path` on the machine running Packer. |

The post-processor needs the builder artifact; it cannot be used with another builder.

## Hard constraints (check these before writing a template)

1. **Packer must run inside the Superphenix subnet (or behind a VPN).** It connects (SSH, or WinRM for Windows)
   to the *private* IP of the build VM, and over SSH to the helper's. No EIP support. Outside the network the build hangs ~10 min then fails with
   `ssh to helper ...`.
2. **Exactly one source**: `source_url` (http(s) URL of a cloud image `.img`/`.qcow2`) **or** `iso_url` (http(s)
   URL of an installer ISO; Superphenix downloads it, so no local path and no `iso_checksum`). ISO installs
   are only tested with Ubuntu Server 26.04 (see "ISO installs" below). Superphenix downloads every URL
   itself (`source_url`, `iso_url`, `cd_url`, `helper_image_url`): they must be reachable from Superphenix.
3. **Windows works with `cd_url`; read `docs/windows.md` first** (validated with Windows Server 2025 FR, about 37 min, 43 min with the qcow2 export; booting the exported qcow2 is not tested). Superphenix imports disks from a URL it can reach, not from the runner's private IP, so a generated ISO (`cd_files`/`cd_content`) cannot be imported from a private runner. Use `cd_url` with a hosted answer-file ISO (`example/windows/answer-iso/make-answer-iso.sh`); the ISO carries the WinRM bootstrap, so it does not depend on the runner (no `BOOTSTRAP_URL`, no `http_*`, no security group rule) and serves any runner. Set `winrm_final_username`/`winrm_final_password` to get your own admin account: the build account (`packer`, throwaway password) is deleted at the image's first boot. The Windows installer has no curl/PowerShell and an AZERTY keyboard on French ISOs. `floppy_*` is not supported.
4. **Sizes are whole GiB**: `memory = "4Gi"` or `"4"`, `disk_size = "20Gi"` or `"20"` (1 to 5000). No `4096Mi`, `4G`, `1.5Gi`: they are rejected. The plugin sends `memory` as `"4Gi"` to Superphenix 0.10+ and as a bare number to older versions.
5. The token is a secret: pass it with `variable "token" { sensitive = true }` fed by `PKR_VAR_token`
   (masked CI variable). Never write it in a template, a log, a commit or a message.
6. One build at a time per project (use GitLab `resource_group`).

## Required arguments

Builder: `api_url`, `org_id`, `az`, `project_id`, `token`, `subnet_eid`, `storage_class`, and one of `source_url` / `iso_url`.
Optional: `cpu` (2), `memory` ("4Gi"), `disk_size` ("20Gi"), `vm_type`, `user_data`, `boot_command`, `boot_wait` (typed over VNC; also with `source_url` for an image without cloud-init), ISO only: `iso_disk_size` ("8Gi"), `disk_bus` ("virtio", `sata` for Windows), `efi` (true), `secure_boot`, `tpm`, `http_content`/`http_directory`, `cd_url`/`cd_disk_size` (hosted answer-file ISO), `cd_files`/`cd_content`/`cd_label`. Communicator: `communicator = "ssh"` with the usual `ssh_*` settings (`ssh_username` defaults to `spx`; with a cloud image the plugin creates that user itself with passwordless sudo), or `communicator = "winrm"` with `winrm_username` and `winrm_password` (Windows); `winrm_final_username` + `winrm_final_password` (together, WinRM only, `sensitive`) create your own administrator after the provisioners and delete the build account at the image's first boot. If the name is the built-in administrator (`Administrator`/`Administrateur`) that account gets the password and is enabled; any other name creates a new account and the built-in administrator is disabled (random password). Details: `docs/windows.md`, "The final account".

Post-processor: `api_url`, `org_id`, `az`, `project_id`, `token`, `subnet_eid`, `storage_class`, `output_path`,
`helper_image_url` (any cloud-init Linux cloud image that can install `qemu-utils`; it must NOT be the build image: two disks with the same filesystem labels/UUIDs make the helper mount the wrong one).
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

locals {
  image = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
  # Not the build image: two disks with the same labels/UUIDs make the helper mount the wrong one.
  helper_image = "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2"
}

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
    helper_image_url = local.helper_image
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
| `cd_files`/`cd_content` | only if Superphenix can reach the runner; otherwise `cd_url` (hosted ISO) |
| `floppy_*` | not supported |
| `shutdown_command` | drop (the plugin stops the VM through the API) |
| `ssh_*` | keep `communicator = "ssh"` and `ssh_username`; with a cloud image the plugin authorizes a generated key (no password); with an ISO your answer file creates the user |
| `winrm_*` | keep `communicator = "winrm"`; the answer file must create the account and enable WinRM; `winrm_final_*` replaces the build account at the end |
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
- Windows: see `docs/windows.md` (answer-file ISO, hosting, `validate-answer-iso.py`).

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
| `output_path ... already exists` | Remove the file or run `packer build -force`. |
| `not enough free space for <output_path>` | The Packer machine's volume cannot hold the qcow2 (6 GB for Ubuntu 26.04, 13 GB for Windows Server 2025). Free space or change `output_path`. The plugin checks before the download. |

Failed or cancelled runs delete their resources. If Packer was killed (SIGKILL, runner crash), orphans may
remain: look for VMs/disks labelled `user.superphenix.net/created-by:packer*` (builder: `packer`, helper:
`packer-helper-<timestamp>`) and delete them **only after confirming with the user**.

## Rules for agents

- Never print, log or commit the token or any `PKR_VAR_token` value.
- Do not invent arguments: only those listed above exist; check `docs/` when unsure.
- Creating, deleting or listing real Superphenix resources (API calls, orphan cleanup) needs the user's go-ahead.
- Prefer `packer validate` before `packer build`; a build creates billable resources.
