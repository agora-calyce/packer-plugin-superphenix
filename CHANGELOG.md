# Changelog

## Unreleased

### Fixed
- Builder: fail at once, with a clear error, when Packer's cache directory (`PACKER_CACHE_DIR`) cannot be created. The SDK's HTTP server step otherwise retried opening its port lock file in a tight loop (about 800 log lines per second), hanging the build and filling `/tmp`.

### Added
- Release files prepared but inactive in `release/` (goreleaser config, release and CI workflows, activation procedure).
- Docs: Linux and Windows user guides (`docs/linux.md`, `docs/windows.md`), `example/windows/` (template, answer-file ISO generator and validator, sample `autounattend.xml`), reference of `boot_command`, `http_*`, `cd_*` and WinRM options, size formats (`4Gi` or `4`).
- Post-processor: the export stops at once on a local write error (full disk) and checks the free space of the output volume before downloading the qcow2, instead of retrying 20 times and reporting a stall.
- Windows (validated with Server 2025 FR, including the qcow2 export: 43 min, 11.6 GiB file): `cd_url`/`cd_disk_size` (hosted answer-file ISO), `cd_files`/`cd_content`/`cd_label` (generated ISO, needs a reachable runner and xorriso), `disk_bus`, WinRM communicator, `vm_type` keeps its own EFI/Secure Boot/TPM defaults. VNC keepalive.
- Builder: `user_data` option. With `source_url`, extra `#cloud-config` merged with the plugin's cloud-init. With
  `iso_url`, passed as is on the instance's cloud-init (answer file for an installer, no network needed).
- Builder: ISO source (`iso_url`, `iso_disk_size`, `efi`, `secure_boot`, `tpm`): empty disk + ISO CD-ROM, UEFI,
  `boot_command`/`boot_wait` typed over the VNC console (RFB over WebSocket), `http_content`/`http_directory`.
  The ISO disk is removed at the end of the build. Tested with Ubuntu Server 26.04, including the export
  (23 minutes, 6 GB qcow2).

### Fixed

- Post-processor: the parent directory of `output_path` is created (it failed with `no such file or directory` after the whole export when it did not exist).
- Builder: resources are now always deleted when a build does not complete (a cancellation could leave the VM
  and disks behind).
- Disk deletion is retried on transient 5xx errors, for up to 10 minutes (VM deletion propagates slowly).

### Breaking changes

- `superphenix-export`: removed `eip_eid` and `runner_cidr`. EIP exposure is no
  longer supported. Packer must now run inside the Superphenix subnet (or behind
  a VPN) to reach the helper VM's private IP, as it already does for the
  builder. Remove these two keys from existing templates; Packer reports them
  as unknown arguments.
