# Changelog

## Unreleased

### Added

- Builder: `user_data` option. With `source_url`, extra `#cloud-config` merged with the plugin's cloud-init. With
  `iso_url`, passed as is on the instance's cloud-init (answer file for an installer, no network needed).
- Builder: ISO source (`iso_url`, `iso_disk_size`, `efi`, `secure_boot`, `tpm`): empty disk + ISO CD-ROM, UEFI,
  `boot_command`/`boot_wait` typed over the VNC console (RFB over WebSocket), `http_content`/`http_directory`.
  The ISO disk is removed at the end of the build. Tested with Ubuntu Server 26.04.

### Fixed

- Builder: resources are now always deleted when a build does not complete (a cancellation could leave the VM
  and disks behind).
- Disk deletion is retried on transient 5xx errors, for up to 10 minutes (VM deletion propagates slowly).

### Breaking changes

- `superphenix-export`: removed `eip_eid` and `runner_cidr`. EIP exposure is no
  longer supported. Packer must now run inside the Superphenix subnet (or behind
  a VPN) to reach the helper VM's private IP, as it already does for the
  builder. Remove these two keys from existing templates; Packer reports them
  as unknown arguments.
