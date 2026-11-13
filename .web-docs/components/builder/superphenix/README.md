Type: `superphenix`
Artifact BuilderId: `agora-calyce.superphenix`

The `superphenix` builder creates a boot disk from a cloud image (`source_url`) or installs one from an ISO
(`iso_url`), creates a VM that boots from it, waits for the VM to get an IP address, connects over SSH (Linux) or WinRM
(Windows), runs the provisioners, then stops the VM. The VM is kept so
that the [superphenix-export](/packer/integrations/agora-calyce/superphenix/latest/components/post-processor/export)
post-processor can export its disk. If the build fails or is cancelled, the VM and its disk are deleted.

The machine running Packer must be able to reach the VM's **private** IP address (SSH or WinRM), since the builder
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

- `secure_boot` (\*bool) - Enable UEFI Secure Boot (ISO installs with EFI). Defaults to `false`,
  or to what `vm_type` defines when `vm_type` is set and none of `efi`,
  `secure_boot` and `tpm` is.

- `tpm` (\*bool) - Add a TPM device to the VM (ISO installs). Defaults to `false`, or to
  what `vm_type` defines (see `secure_boot`).

- `cd_url` (string) - URL of a ready-made ISO (an `http` disk source that Superphenix downloads
  itself) attached as a second CD-ROM, typically an answer-file ISO
  (`autounattend.xml`) for Windows. Needs `iso_url`; exclusive with
  `cd_files` and `cd_content`. Use it when Superphenix cannot reach the
  Packer machine to import a generated ISO.

- `cd_disk_size` (string) - Size of the `cd_url` disk, larger than the ISO. Defaults to `1Gi`. Whole GiB only, between 1 and 5000: write `1Gi` or `1` (other units such as `Mi` and fractions are rejected; the plugin sends the bare number to the API).

- `disk_bus` (string) - Bus of the system disk of an ISO install: `virtio` (default) or `sata`.
  Windows has no virtio storage driver during setup, so use `sata` for it.

- `iso_disk_size` (string) - Size of the disk holding the ISO, larger than the ISO. Defaults to `8Gi`. Whole GiB only, between 1 and 5000: write `8Gi` or `8` (other units such as `Mi` and fractions are rejected; the plugin sends the bare number to the API).

- `cpu` (int) - Number of vCPUs. Defaults to `2`.

- `memory` (string) - Amount of memory. Defaults to `4Gi`. Whole GiB only, between 1 and 5000: write `4Gi` or `4` (other units such as `Mi` and fractions are rejected). The plugin sends `memory` to the API as a quantity (`4Gi`), or as a bare number to a Superphenix older than 0.10.

- `disk_size` (string) - Size of the boot disk. Defaults to `20Gi`. Whole GiB only, between 1 and 5000: write `20Gi` or `20` (other units such as `Mi` and fractions are rejected; the plugin sends the bare number to the API).

- `user_data` (string) - Cloud-init user-data (`#cloud-config` YAML), for example
  `file("user-data.yml")`, set on the instance's CloudInit configuration.
  With `source_url` it is merged with the plugin's own config: `users` and
  `packages` are appended (the SSH user is always created), other keys
  override. With `iso_url` it is passed as is, and is the way to give an
  installer its answer file without any network access from the VM, for
  example an Ubuntu `autoinstall:` section (keep `autoinstall` on the
  kernel command line). Other formats (shell script, MIME) are not supported.

- `vm_type` (string) - VM type (size preset), if your AZ defines some.

- `winrm_final_username` (string) - WinRM builds: name of the administrator account created once the provisioners are done. The build account
  (`winrm_username`, created by the answer file) is then deleted at the first boot of the image, so the image
  keeps only this account. Needs `winrm_final_password`. Leave both empty to keep the build account.

- `winrm_final_password` (string) - Password of `winrm_final_username`. Pass it through a sensitive variable. It is never written to the
  image in clear text and is masked in the logs.

<!-- End of code generated from the comments of the Config struct in builder/builder.go; -->


### HTTP server

`http_content`, `http_directory` and the port options serve files to the VM (`{{ .HTTPIP }}` and
`{{ .HTTPPort }}` in `boot_command`). The VM must be able to reach the Packer machine on that port.

<!-- Code generated from the comments of the HTTPConfig struct in multistep/commonsteps/http_config.go; DO NOT EDIT MANUALLY -->

- `http_directory` (string) - Path to a directory to serve using an HTTP server. The files in this
  directory will be available over HTTP that will be requestable from the
  virtual machine. This is useful for hosting kickstart files and so on.
  By default this is an empty string, which means no HTTP server will be
  started. The address and port of the HTTP server will be available as
  variables in `boot_command`. This is covered in more detail below.

- `http_content` (map[string]string) - Key/Values to serve using an HTTP server. `http_content` works like and
  conflicts with `http_directory`. The keys represent the paths and the
  values contents, the keys must start with a slash, ex: `/path/to/file`.
  `http_content` is useful for hosting kickstart files and so on. By
  default this is empty, which means no HTTP server will be started. The
  address and port of the HTTP server will be available as variables in
  `boot_command`. This is covered in more detail below.
  Example:
  ```hcl
    http_content = {
      "/a/b"     = file("http/b")
      "/foo/bar" = templatefile("${path.root}/preseed.cfg", { packages = ["nginx"] })
    }
  ```

- `http_port_min` (int) - These are the minimum and maximum port to use for the HTTP server
  started to serve the `http_directory`. Because Packer often runs in
  parallel, Packer will choose a randomly available port in this range to
  run the HTTP server. If you want to force the HTTP server to be on one
  port, make this minimum and maximum port the same. By default the values
  are `8000` and `9000`, respectively.

- `http_port_max` (int) - HTTP Port Max

- `http_bind_address` (string) - This is the bind address for the HTTP server. Defaults to 0.0.0.0 so that
  it will work with any network interface.

- `http_network_protocol` (string) - Defines the HTTP Network protocol. Valid options are `tcp`, `tcp4`, `tcp6`,
  `unix`, and `unixpacket`. This value defaults to `tcp`.

<!-- End of code generated from the comments of the HTTPConfig struct in multistep/commonsteps/http_config.go; -->


### Boot command

`boot_command` is typed on the VM's VNC console, after `boot_wait`. It also works with `source_url`, for an image
without cloud-init that must be set up from its console (enable SSH, set a password). The keyboard layout is
the installer's: on a French Windows installer, keys are mapped as on an AZERTY keyboard.

<!-- Code generated from the comments of the BootConfig struct in bootcommand/config.go; DO NOT EDIT MANUALLY -->

- `boot_keygroup_interval` (duration string | ex: "1h5m2s") - Time to wait after sending a group of key pressses. The value of this
  should be a duration. Examples are `5s` and `1m30s` which will cause
  Packer to wait five seconds and one minute 30 seconds, respectively. If
  this isn't specified, a sensible default value is picked depending on
  the builder type.

- `boot_wait` (duration string | ex: "1h5m2s") - The time to wait after booting the initial virtual machine before typing
  the `boot_command`. The value of this should be a duration. Examples are
  `5s` and `1m30s` which will cause Packer to wait five seconds and one
  minute 30 seconds, respectively. If this isn't specified, the default is
  `10s` or 10 seconds. To set boot_wait to 0s, use a negative number, such
  as "-1s"

- `boot_command` ([]string) - This is an array of commands to type when the virtual machine is first
  booted. The goal of these commands should be to type just enough to
  initialize the operating system installer. Special keys can be typed as
  well, and are covered in the section below on the boot command. If this
  is not specified, it is assumed the installer will start itself.

<!-- End of code generated from the comments of the BootConfig struct in bootcommand/config.go; -->


### CD (generated ISO)

`cd_files`, `cd_content` and `cd_label` generate an ISO on the Packer machine (`xorriso`, `mkisofs` or similar
required) that Superphenix then imports from Packer's HTTP server: this only works if Superphenix can reach
the Packer machine. Otherwise host a ready-made ISO and use `cd_url`.

<!-- Code generated from the comments of the CDConfig struct in multistep/commonsteps/extra_iso_config.go; DO NOT EDIT MANUALLY -->

- `cd_files` ([]string) - A list of files to place onto a CD that is attached when the VM is
  booted. This can include either files or directories; any directories
  will be copied onto the CD recursively, preserving directory structure
  hierarchy. Symlinks will have the link's target copied into the directory
  tree on the CD where the symlink was. File globbing is allowed.
  
  Usage example (JSON):
  
  ```json
  "cd_files": ["./somedirectory/meta-data", "./somedirectory/user-data"],
  "cd_label": "cidata",
  ```
  
  Usage example (HCL):
  
  ```hcl
  cd_files = ["./somedirectory/meta-data", "./somedirectory/user-data"]
  cd_label = "cidata"
  ```
  
  The above will create a CD with two files, user-data and meta-data in the
  CD root. This specific example is how you would create a CD that can be
  used for an Ubuntu 20.04 autoinstall.
  
  Since globbing is also supported,
  
  ```hcl
  cd_files = ["./somedirectory/*"]
  cd_label = "cidata"
  ```
  
  Would also be an acceptable way to define the above cd. The difference
  between providing the directory with or without the glob is whether the
  directory itself or its contents will be at the CD root.
  
  Use of this option assumes that you have a command line tool installed
  that can handle the iso creation. Packer will use one of the following
  tools:
  
    * xorriso
    * mkisofs
    * hdiutil (normally found in macOS)
    * oscdimg (normally found in Windows as part of the Windows ADK)

- `cd_content` (map[string]string) - Key/Values to add to the CD. The keys represent the paths, and the values
  contents. It can be used alongside `cd_files`, which is useful to add large
  files without loading them into memory. If any paths are specified by both,
  the contents in `cd_content` will take precedence.
  
  Usage example (HCL):
  
  ```hcl
  cd_files = ["vendor-data"]
  cd_content = {
    "meta-data" = jsonencode(local.instance_data)
    "user-data" = templatefile("user-data", { packages = ["nginx"] })
  }
  cd_label = "cidata"
  ```

- `cd_label` (string) - CD Label

<!-- End of code generated from the comments of the CDConfig struct in multistep/commonsteps/extra_iso_config.go; -->


### Communicator

The builder supports the SSH and WinRM communicators.

**SSH** (Linux). The default SSH user is `spx`. A temporary key pair is generated for each build, and a
cloud-init configuration creates that user (with passwordless `sudo`) and authorizes the key. This custom
cloud-init replaces the default one of Superphenix. With `iso_url`, the answer file you provide must create
the user (see `user_data`).

**WinRM** (Windows). Set `communicator = "winrm"` with `winrm_username` and `winrm_password`: the answer file
of the installer must create that account and enable WinRM (see the Windows guide). To leave a clean image, also set
`winrm_final_username` and `winrm_final_password` (both, WinRM only): once the provisioners are done the plugin creates
that administrator and schedules the removal of the build account (`winrm_username`) and of the unencrypted Basic
authentication at the first boot of a VM deployed from the image.

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


<!-- Code generated from the comments of the WinRM struct in communicator/config.go; DO NOT EDIT MANUALLY -->

- `winrm_username` (string) - The username to use to connect to WinRM.

- `winrm_password` (string) - The password to use to connect to WinRM.

- `winrm_host` (string) - The address for WinRM to connect to.
  
  NOTE: If using an Amazon EBS builder, you can specify the interface
  WinRM connects to via
  [`ssh_interface`](/packer/integrations/agora-calyce/amazon/latest/components/builder/ebs#ssh_interface)

- `winrm_no_proxy` (bool) - Setting this to `true` adds the remote
  `host:port` to the `NO_PROXY` environment variable. This has the effect of
  bypassing any configured proxies when connecting to the remote host.
  Default to `false`.

- `winrm_port` (int) - The WinRM port to connect to. This defaults to `5985` for plain
  unencrypted connection and `5986` for SSL when `winrm_use_ssl` is set to
  true.

- `winrm_timeout` (duration string | ex: "1h5m2s") - The amount of time to wait for WinRM to become available. This defaults
  to `30m` since setting up a Windows machine generally takes a long time.

- `winrm_retry_interval` (duration string | ex: "1h5m2s") - The amount of time to wait between retries when attempting to connect to
  WinRM. This defaults to `5s`.

- `winrm_connect_timeout` (duration string | ex: "1h5m2s") - The timeout for each individual WinRM connection attempt. This defaults
  to `0` (no per-attempt timeout; each attempt waits until the server
  responds or resets the connection).

- `winrm_use_ssl` (bool) - If `true`, use HTTPS for WinRM.

- `winrm_insecure` (bool) - If `true`, do not check server certificate chain and host name.

- `winrm_use_ntlm` (boolean) - If `true`, NTLMv2 authentication (with session security) will be used
  for WinRM, rather than default (basic authentication), removing the
  requirement for basic authentication to be enabled within the target
  guest. Further reading for remote connection authentication can be found
  [here](https://msdn.microsoft.com/en-us/library/aa384295(v=vs.85).aspx).

<!-- End of code generated from the comments of the WinRM struct in communicator/config.go; -->


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

## Windows Server (answer-file ISO)

```hcl
source "superphenix" "win2025" {
  # api_url, org_id, az, project_id, token, subnet_eid, storage_class as above
  vm_type  = "windows.2k25"  # sets UEFI, Secure Boot and TPM like the console does
  disk_bus = "sata"          # no virtio storage driver during Windows setup
  iso_url  = "https://example.com/windows-server-2025.iso"
  cd_url   = "http://<host>/<random>/answers.iso"  # ISO holding autounattend.xml, hosted where Superphenix can download it

  cpu       = 4
  memory    = "8Gi"
  disk_size = "50Gi"

  boot_wait    = "5s"
  boot_command = ["<spacebar><wait3><spacebar><wait3><spacebar><wait3><spacebar><wait3><enter>"]

  communicator   = "winrm"
  winrm_username = "packer"          # build account created by the answer-file ISO
  winrm_password = var.win_password
  winrm_final_username = "ops"       # created after the provisioners; the build account is deleted at the image's first boot
  winrm_final_password = var.final_password
  winrm_use_ssl  = false
  winrm_insecure = true
  winrm_timeout  = "2h"
}
```

How to build and host the answer-file ISO is in the Windows guide (`docs/windows.md` in the plugin repository);
the Linux guide is `docs/linux.md`.

## Guides

Step-by-step guides in the plugin repository: [Linux](https://github.com/agora-calyce/packer-plugin-superphenix/blob/main/docs/linux.md)
(cloud image, ISO, `user_data`, export) and [Windows](https://github.com/agora-calyce/packer-plugin-superphenix/blob/main/docs/windows.md)
(answer-file ISO, hosting it, WinRM).

## Limitations

- Linux ISO installs are tested with Ubuntu Server 26.04 (EFI, `user_data` autoinstall). Other installers, Secure
  Boot and `tpm` are untested.
- Windows is validated with Windows Server 2025 (French ISO) only: a full build (ISO install, WinRM,
  provisioning) takes about 37 minutes, about 43 with the qcow2 export. Superphenix imports disks from a URL it can reach, so on a runner with a
  private IP use `cd_url` instead of `cd_files` / `cd_content`. `floppy_*` is not supported.
- The `superphenix-export` post-processor is validated on a VM built from a cloud image and on VMs built from an ISO (Ubuntu Server 26.04,
  Windows Server 2025). The exported disk of an ISO build holds an EFI system: boot it with an EFI firmware (booting the exported qcow2 is not tested).
- Sizes (`memory`, `disk_size`, ...) are whole GiB, written `4Gi` or `4`.
