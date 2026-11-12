# Building Linux images

Two ways to get a Linux image: customize an existing **cloud image**, or install from an **ISO** with an
unattended installer. Both end the same way: provisioners over SSH, the VM stopped and kept, then the optional
qcow2 export. For Windows, see [windows.md](windows.md).

Templates: [`example/ubuntu.pkr.hcl`](../example/ubuntu.pkr.hcl) (cloud image) and
[`example/ubuntu-iso.pkr.hcl`](../example/ubuntu-iso.pkr.hcl) (ISO, tested with Ubuntu Server 26.04).

## Prerequisites

- Packer runs on a VM **inside the subnet** (or behind a VPN): the plugin reaches the build VM, and the export
  helper, on their **private IPs** over SSH. Exposing them through an EIP is not supported.
- A Superphenix API URL, organization, availability zone, project and an API token (console, "API tokens"),
  passed through a sensitive variable (`PKR_VAR_token`).
- A subnet (its effective ID) and a storage class (listed by `GET .../{az}/{project_id}/storage-class`).
- Superphenix must be able to download `source_url` / `iso_url`: a direct `http(s)` link, reachable from the
  Internet. Local files and checksums are not supported.

## Option A: cloud image (`source_url`)

The image is imported as the boot disk, and the plugin gives it a cloud-init that creates the SSH user (`spx`
by default, passwordless `sudo`) with a temporary key generated for the build.

```hcl
source "superphenix" "ubuntu" {
  api_url       = var.api_url
  org_id        = var.org_id
  az            = var.az
  project_id    = var.project_id
  token         = var.token
  subnet_eid    = var.subnet_eid
  storage_class = "block-standard"

  source_url = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
  cpu        = 2
  memory     = "4Gi"
  disk_size  = "20Gi"

  communicator = "ssh"
  ssh_username = "spx"
}

build {
  sources = ["source.superphenix.ubuntu"]
  provisioner "shell" {
    inline = ["sudo apt-get update", "sudo apt-get install -y nginx"]
  }
}
```

- **`user_data`** (`#cloud-config` YAML) is **merged** with the plugin's cloud-init: your `users` and `packages`
  lists are appended (the SSH user always exists), and every other key you set wins. Only `#cloud-config` is
  accepted. Prefer provisioners for anything that can run once SSH is up.
- Any cloud-init image should work if its default user can be created by cloud-init. If the user is not
  `spx`, set `ssh_username`.
- `vm_type` selects a Superphenix instance type, if you want one.

## Option B: install from an ISO (`iso_url`)

The plugin creates an empty `disk_size` disk and attaches the ISO as a CD-ROM, boots with **UEFI** (with BIOS the
CD-ROM is never booted), and types `boot_command` on the VM's VNC console. After the installer reboots onto
the disk, the build continues over SSH, and the ISO disk is deleted at the end.

### The answer file: `user_data`

Give the installer its answer file through `user_data`: it is set on the instance's cloud-init configuration,
so the installer reads it from the VM's cloud-init disk with no network access. In ISO mode it is passed **as
is**, with no merge and no SSH user added: your answer file must create the user that Packer connects with and
enable SSH. For Ubuntu Server (autoinstall):

```hcl
user_data = <<-EOT
  #cloud-config
  autoinstall:
    version: 1
    locale: en_US.UTF-8
    keyboard: {layout: us}
    storage: {layout: {name: direct}}
    ssh: {install-server: true, allow-pw: true}
    identity:
      hostname: ubuntu-iso
      username: adminsys
      password: "${var.ssh_password_hash}"   # mkpasswd -m sha-512
    user-data:
      users:
        - name: adminsys
          sudo: ALL=(ALL) NOPASSWD:ALL
          shell: /bin/bash
          lock-passwd: false
EOT
```

`http_content` and `http_directory` also work (`{{ .HTTPIP }}` and `{{ .HTTPPort }}` in `boot_command`), but the
VM must reach the Packer machine on that port, which a security group may block (`http_port_min`,
`http_port_max`). Prefer `user_data`.

### `boot_command`

For Ubuntu live-server, the installer must start with `autoinstall` on the kernel command line, **before** the
`---` of the `linux` line:

```hcl
boot_wait = "2s"
boot_command = [
  "<spacebar><wait><spacebar><wait><spacebar><wait>",                  # "press any key" prompt
  "e<wait><down><down><down><end><left><left><left><left>",            # GRUB editor, cursor before ---
  "autoinstall ",
  "<wait><f10>",
]
```

This depends on the ISO's GRUB menu (checked with 26.04). When you change release or distribution, look at the
GRUB screen through the Superphenix console (VNC) instead of guessing the key sequence.

### SSH and options

```hcl
communicator = "ssh"
ssh_username = "adminsys"
ssh_password = var.ssh_password
ssh_timeout  = "40m"      # the install itself takes most of this
```

Useful options: `iso_disk_size` (whole GiB larger than the ISO, default `8Gi`), `efi` (default true),
`secure_boot`, `tpm`, `disk_bus`, `vm_type`. A full Ubuntu 26.04 build took about 18 minutes, 23 with the qcow2 export.

## Export

Add the post-processor to get a qcow2 (see [`docs/post-processors/export.mdx`](post-processors/export.mdx)).
It starts a small helper VM, converts the build disk with `qemu-img` and downloads the file over SSH, then
deletes the helper:

```hcl
post-processor "superphenix-export" {
  api_url          = var.api_url
  org_id           = var.org_id
  az               = var.az
  project_id       = var.project_id
  token            = var.token
  subnet_eid       = var.subnet_eid
  storage_class    = "block-standard"
  helper_image_url = "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2" # not the build image (see below)
  output_path      = "output/image.qcow2"
}
```

The export is validated on a VM built from a cloud image, and on VMs built from an ISO (Ubuntu Server 26.04: 6 GB
qcow2, 23 minutes with the export; Windows Server 2025). A disk built from an ISO holds an EFI system, so boot the
qcow2 with UEFI. `helper_image_url` must not be the image the build used (the helper would see two disks with the same
labels and UUIDs), and an existing `output_path` is refused unless Packer runs with `-force`. The Packer machine
needs free disk space for the qcow2 (the size of the used data, for example about 13 GB for Windows Server 2025). The plugin checks it before the download and stops with a clear message if it is not enough.

## Troubleshooting

| Symptom | Cause / action |
|---|---|
| `exactly one of source_url and iso_url is required` | Set only one source. |
| ISO build stuck at "No bootable device" | The firmware is BIOS: leave `efi` unset (default true). |
| The installer asks questions | `autoinstall` is missing from the kernel line, or typed after `---`: look at the GRUB screen. |
| Build SSH timeout | The runner cannot reach the VM's private IP (outside the subnet/VPN), or the answer file did not create the SSH user / enable password SSH. |
| ISO build: SSH timeout and no installer activity | Look at the VM's console (VNC) before changing anything. The GRUB key sequence of `boot_command` is timing sensitive: one Ubuntu 26.04 run timed out after 40 minutes with no explanation, and the identical run succeeded right after. Retry once, then check the screen. |
| `ssh to helper <ip>: ...` | Same, for the export helper. |
| `output_path ... already exists` | Remove the file or run `packer build -force`. |
| `not enough free space for ...` | The volume of `output_path` cannot hold the qcow2. Free space or point `output_path` to a larger volume. |
| Helper never gets `qemu-img` | The subnet needs Internet access to the distribution mirrors. |
| `memory: ...` / `disk_size: ...` error | Whole GiB values like `"4Gi"`. |
| `401` / `403` from the API | Wrong or expired token, or missing rights on the project. Do not print it while debugging. |
| Export disk not found | Set `disk_device` (for example `/dev/vdb`). |
| Resources left after a failure | The plugin deletes the VM and disks on failure or Ctrl-C. Disk deletion can answer 500 for a few minutes after the VM is deleted, and is retried. |
