
# Superphenix Builder

Type: `superphenix`
Artifact BuilderId: `agora-calyce.superphenix`

The `superphenix` builder creates a boot disk from a cloud image (`source_url`) or installs one from an ISO
(`iso_url`), creates a VM that boots from it, waits for the VM to get an IP address, connects over SSH, runs the
provisioners, then stops the VM. The VM is kept so
that the [superphenix-export](/packer/integrations/agora-calyce/superphenix/latest/components/post-processor/export)
post-processor can export its disk. If the build fails or is cancelled, the VM and its disk are deleted.

The machine running Packer must be able to reach the VM's **private** IP address over SSH, since the builder
does not expose the VM publicly. In practice, run Packer on a VM inside the subnet (or behind a VPN).

## Sources

Exactly one of `source_url` and `iso_url` is required.

- **Cloud image** (`source_url`): the image is imported as the boot disk. `user_data` (`#cloud-config`) is merged
  with the plugin's own cloud-init, which always creates the SSH user.
- **ISO** (`iso_url`): Superphenix imports the ISO from the URL (local files and checksums are not supported)
  and the VM boots it as a CD-ROM next to an empty `disk_size` disk. The installer is driven with `boot_command`,
  typed over the VM's VNC console after `boot_wait`. The VM uses **UEFI** (`efi`, default `true`): with the
  BIOS firmware the CD-ROM is never booted. Once the installed system reboots on the disk, the build continues
  over SSH. The ISO disk is detached and deleted at the end.

For an unattended install, pass the answer file through `user_data`: it is set on the instance's cloud-init
configuration, so the installer reads it from the VM's cloud-init disk and the VM does not need to reach the
machine running Packer. In ISO mode it is passed as is and is not merged with anything.
`http_content` and `http_directory` also work (`{{ .HTTPIP }}` and `{{ .HTTPPort }}` in `boot_command`), but
the VM must be able to reach the Packer machine on that port, which a security group may block (see
`http_port_min` and `http_port_max`).

## Configuration Reference

There are many configuration options available for the builder. They are segmented below into two
categories: required and optional parameters. Within each category, the available configuration keys are
alphabetized.

### Required:

<!-- Code generated from the comments of the Config struct in builder/builder.go; DO NOT EDIT MANUALLY -->

- `api_url` (string) - Base URL of the Superphenix API, for example `https://api.superphenix.example`.

- `org_id` (string) - ID of the organization that owns the project.

- `az` (string) - Availability zone in which the VM is built.

- `project_id` (string) - ID of the project in which the VM is built.

- `token` (string) - API token created from the console "API tokens" page, sent as
  `Authorization: api-token <token>`. Pass it through a sensitive
  variable (`PKR_VAR_token`) and never write it in the template.

- `storage_class` (string) - Storage class of the boot disk, for example `block-standard`. The
  available classes are listed by `GET .../{az}/{project_id}/storage-class`.

- `subnet_eid` (string) - Effective ID of the subnet the VM is attached to. The machine running
  Packer must be able to reach the VM's IP address.

<!-- End of code generated from the comments of the Config struct in builder/builder.go; -->


### Optional:

<!-- Code generated from the comments of the Config struct in builder/builder.go; DO NOT EDIT MANUALLY -->

- `source_url` (string) - URL of the cloud image used to create the boot disk (an `http` disk
  source). Exactly one of `source_url` and `iso_url` is required.

- `iso_url` (string) - URL of an installation ISO (an `http` disk source that Superphenix
  downloads itself; local paths and checksums are not supported). The
  VM boots an empty `disk_size` disk with the ISO as a CD-ROM, so the
  installer must be driven with `boot_command` and `http_content` /
  `http_directory`. The ISO is detached and deleted once the build is done.

- `efi` (\*bool) - UEFI firmware for ISO installs. Defaults to `true`: BIOS only tries the
  hard disk, so the ISO would never boot. Windows needs EFI anyway.

- `secure_boot` (bool) - Enable UEFI Secure Boot (ISO installs with EFI). Defaults to `false`.

- `tpm` (bool) - Add a TPM device to the VM (ISO installs). Defaults to `false`.

- `iso_disk_size` (string) - Size of the disk holding the ISO, a whole number of GiB larger than the
  ISO. Defaults to `8Gi`.

- `cpu` (int) - Number of vCPUs. Defaults to `2`.

- `memory` (string) - Amount of memory, a whole number of GiB such as `4Gi`. Defaults to `4Gi`.

- `disk_size` (string) - Size of the boot disk, a whole number of GiB such as `20Gi`. Defaults to `20Gi`.

- `user_data` (string) - Cloud-init user-data (`#cloud-config` YAML), for example
  `file("user-data.yml")`, set on the instance's CloudInit configuration.
  With `source_url` it is merged with the plugin's own config: `users` and
  `packages` are appended (the SSH user is always created), other keys
  override. With `iso_url` it is passed as is, and is the way to give an
  installer its answer file without any network access from the VM, for
  example an Ubuntu `autoinstall:` section (keep `autoinstall` on the
  kernel command line). Other formats (shell script, MIME) are not supported.

- `vm_type` (string) - VM type (size preset), if your AZ defines some.

<!-- End of code generated from the comments of the Config struct in builder/builder.go; -->


### Communicator

The builder supports the SSH communicator. The default SSH user is `spx`. A temporary key pair is generated
for each build, and a cloud-init configuration creates that user (with passwordless `sudo`) and authorizes the
key. This custom cloud-init replaces the default one of Superphenix.

<!-- Code generated from the comments of the Config struct in communicator/config.go; DO NOT EDIT MANUALLY -->

- `communicator` (string) - Packer currently supports three kinds of communicators:
  
  -   `none` - No communicator will be used. If this is set, most
      provisioners also can't be used.
  
  -   `ssh` - An SSH connection will be established to the machine. This
      is usually the default.
  
  -   `winrm` - A WinRM connection will be established.
  
  In addition to the above, some builders have custom communicators they
  can use. For example, the Docker builder has a "docker" communicator
  that uses `docker exec` and `docker cp` to execute scripts and copy
  files.

- `pause_before_connecting` (duration string | ex: "1h5m2s") - We recommend that you enable SSH or WinRM as the very last step in your
  guest's bootstrap script, but sometimes you may have a race condition
  where you need Packer to wait before attempting to connect to your
  guest.
  
  If you end up in this situation, you can use the template option
  `pause_before_connecting`. By default, there is no pause. For example if
  you set `pause_before_connecting` to `10m` Packer will check whether it
  can connect, as normal. But once a connection attempt is successful, it
  will disconnect and then wait 10 minutes before connecting to the guest
  and beginning provisioning.

<!-- End of code generated from the comments of the Config struct in communicator/config.go; -->


<!-- Code generated from the comments of the SSH struct in communicator/config.go; DO NOT EDIT MANUALLY -->

- `ssh_host` (string) - The address to SSH to. This usually is automatically configured by the
  builder.

- `ssh_port` (int) - The port to connect to SSH. This defaults to `22`.

- `ssh_username` (string) - The username to connect to SSH with. Required if using SSH.

- `ssh_password` (string) - A plaintext password to use to authenticate with SSH.

- `ssh_ciphers` ([]string) - This overrides the value of ciphers supported by default by Golang.
  The default value is [
    "aes128-gcm@openssh.com",
    "chacha20-poly1305@openssh.com",
    "aes128-ctr", "aes192-ctr", "aes256-ctr",
  ]
  
  Valid options for ciphers include:
  "aes128-ctr", "aes192-ctr", "aes256-ctr", "aes128-gcm@openssh.com",
  "chacha20-poly1305@openssh.com",
  "arcfour256", "arcfour128", "arcfour", "aes128-cbc", "3des-cbc",

- `ssh_clear_authorized_keys` (bool) - If true, Packer will attempt to remove its temporary key from
  `~/.ssh/authorized_keys` and `/root/.ssh/authorized_keys`. This is a
  mostly cosmetic option, since Packer will delete the temporary private
  key from the host system regardless of whether this is set to true
  (unless the user has set the `-debug` flag). Defaults to "false";
  currently only works on guests with `sed` installed.

- `ssh_key_exchange_algorithms` ([]string) - If set, Packer will override the value of key exchange (kex) algorithms
  supported by default by Golang. Acceptable values include:
  "curve25519-sha256@libssh.org", "ecdh-sha2-nistp256",
  "ecdh-sha2-nistp384", "ecdh-sha2-nistp521",
  "diffie-hellman-group14-sha1", and "diffie-hellman-group1-sha1".

- `ssh_certificate_file` (string) - Path to user certificate used to authenticate with SSH.
  The `~` can be used in path and will be expanded to the
  home directory of current user.

- `ssh_pty` (bool) - If `true`, a PTY will be requested for the SSH connection. This defaults
  to `false`.

- `ssh_timeout` (duration string | ex: "1h5m2s") - The time to wait for SSH to become available. Packer uses this to
  determine when the machine has booted so this is usually quite long.
  Example value: `10m`.
  This defaults to `5m`, unless `ssh_handshake_attempts` is set.

- `ssh_disable_agent_forwarding` (bool) - If true, SSH agent forwarding will be disabled. Defaults to `false`.

- `ssh_handshake_attempts` (int) - The number of handshakes to attempt with SSH once it can connect.
  This defaults to `10`, unless a `ssh_timeout` is set.

- `ssh_bastion_host` (string) - A bastion host to use for the actual SSH connection.

- `ssh_bastion_port` (int) - The port of the bastion host. Defaults to `22`.

- `ssh_bastion_agent_auth` (bool) - If `true`, the local SSH agent will be used to authenticate with the
  bastion host. Defaults to `false`.

- `ssh_bastion_username` (string) - The username to connect to the bastion host.

- `ssh_bastion_password` (string) - The password to use to authenticate with the bastion host.

- `ssh_bastion_interactive` (bool) - If `true`, the keyboard-interactive used to authenticate with bastion host.

- `ssh_bastion_private_key_file` (string) - Path to a PEM encoded private key file to use to authenticate with the
  bastion host. The `~` can be used in path and will be expanded to the
  home directory of current user.

- `ssh_bastion_certificate_file` (string) - Path to user certificate used to authenticate with bastion host.
  The `~` can be used in path and will be expanded to the
  home directory of current user.

- `ssh_file_transfer_method` (string) - `scp` or `sftp` - How to transfer files, Secure copy (default) or SSH
  File Transfer Protocol.
  
  **NOTE**: Guests using Windows with Win32-OpenSSH v9.1.0.0p1-Beta, scp
  (the default protocol for copying data) returns a a non-zero error code since the MOTW
  cannot be set, which cause any file transfer to fail. As a workaround you can override the transfer protocol
  with SFTP instead `ssh_file_transfer_method = "sftp"`.

- `ssh_proxy_host` (string) - A SOCKS proxy host to use for SSH connection

- `ssh_proxy_port` (int) - A port of the SOCKS proxy. Defaults to `1080`.

- `ssh_proxy_username` (string) - The optional username to authenticate with the proxy server.

- `ssh_proxy_password` (string) - The optional password to use to authenticate with the proxy server.

- `ssh_keep_alive_interval` (duration string | ex: "1h5m2s") - How often to send "keep alive" messages to the server. Set to a negative
  value (`-1s`) to disable. Example value: `10s`. Defaults to `5s`.

- `ssh_read_write_timeout` (duration string | ex: "1h5m2s") - The amount of time to wait for a remote command to end. This might be
  useful if, for example, packer hangs on a connection after a reboot.
  Example: `5m`. Disabled by default.

- `ssh_remote_tunnels` ([]string) - Remote tunnels forward a port from your local machine to the instance.
  Format: ["REMOTE_PORT:LOCAL_HOST:LOCAL_PORT"]
  Example: "9090:localhost:80" forwards localhost:9090 on your machine to port 80 on the instance.

- `ssh_local_tunnels` ([]string) - Local tunnels forward a port from the instance to your local machine.
  Format: ["LOCAL_PORT:REMOTE_HOST:REMOTE_PORT"]
  Example: "8080:localhost:3000" allows the instance to access your local machine’s port 3000 via localhost:8080.

<!-- End of code generated from the comments of the SSH struct in communicator/config.go; -->


## Example

```hcl
source "superphenix" "ubuntu" {
  api_url    = var.api_url
  org_id     = var.org_id
  az         = var.az
  project_id = var.project_id
  token      = var.token

  source_url = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
  cpu        = 2
  memory     = "4Gi"
  disk_size  = "20Gi"
  subnet_eid = var.subnet_eid
  storage_class = "block-standard"

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

### Ubuntu Server ISO (autoinstall)

```hcl
source "superphenix" "ubuntu-iso" {
  # api_url, org_id, az, project_id, token, subnet_eid, storage_class as above
  iso_url   = "https://releases.ubuntu.com/26.04/ubuntu-26.04-live-server-amd64.iso"
  cpu       = 2
  memory    = "4Gi"
  disk_size = "20Gi"

  user_data = file("${path.root}/user-data.yml") # starts with #cloud-config, has an `autoinstall:` section

  boot_wait = "2s"
  boot_command = [
    "<spacebar><wait><spacebar><wait><spacebar><wait>",
    "e<wait><down><down><down><end><left><left><left><left>",
    "autoinstall ",
    "<wait><f10>",
  ]

  communicator = "ssh"
  ssh_username = "adminsys"
  ssh_password = var.ssh_password
  ssh_timeout  = "40m"
}
```

The key sequence inserts `autoinstall` before the `---` of the `linux` line in GRUB's editor (checked with the
Ubuntu 26.04 live-server ISO). It depends on the ISO's GRUB menu: check it on a screenshot when you change
release. The autoinstall must define the user that Packer connects with and enable SSH.

## Limitations

- ISO installs are tested with Ubuntu Server 26.04 (EFI, `user_data` autoinstall). Other installers, Secure Boot
  and `tpm` are untested.
- Only the SSH communicator is supported. WinRM is not supported yet, so no Windows images yet.
- No `cd_files` / `cd_content` / `floppy_*` yet.
- The `superphenix-export` post-processor has not been re-tested on a VM built from an ISO. The exported disk
  holds an EFI system: boot it with an EFI firmware.
