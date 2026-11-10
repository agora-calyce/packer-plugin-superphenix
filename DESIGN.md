# Packer plugin for Superphenix — design

## Scope
- Go plugin (`packer-plugin-sdk`): **builder** + **post-processor**.
- Builder: creates the VM from a cloud image (`source_url`) or installs from an ISO (`iso_url`), provisions through SSH (Linux) or WinRM (Windows). Windows: hosted answer-file ISO (`cd_url`), validated with Server 2025 FR; `cd_files`/`cd_content` only if Superphenix can reach the runner; no `floppy`.
- Post-processor: exports the disk as qcow2 to the machine running Packer (variant 3b).
- Auth: API token (header `Authorization: api-token <token>`, created in the console) via a masked CI variable. Hand-written HTTP client (no public Go SDK).
- Network: the runner reaches the VMs' **private IPs** (builder and export helper). It must therefore sit in the Superphenix subnet, or behind a VPN. Exposure through an EIP is not supported.
- Distribution: GitHub Releases, `packer init` (`PACKER_GITHUB_API_TOKEN` token if the repository is private).

## Superphenix API used
Base: `{api_url}/{orgId}/api/spx-ctrl/{az}/{projectId}/...`. Spec: `https://docs.superphenix.net/assets/openapi/superphenix-api.json` (source: `internal/superphenix-api/api/docs.go` in super-phenix/superphenix).

| Need | Endpoint |
|---|---|
| Create a disk | `POST /disk` (`storage` = integer in GiB, `storageClass` required, `source.type` = `http`) |
| Wait for import | `GET /disk/{id}` → `disk.status.phase == "Succeeded"` |
| Create VM / helper | `POST /instance` (`network` required, `compute.memory` = quantity string `"4Gi"` since API 0.10 (integer in GiB before), labels `user.superphenix.net/<key>:<value>`) |
| IP / state | `GET /instance/{id}` (`vmi.status.interfaces[].ipAddresses`, `vm.status.printableStatus`) |
| Stop | `GET /instance/{id}/stop` |
| Detach a disk | `GET /disk/{id}/unmount` |
| Attach a disk | at VM creation only: `disks[].eid` |
| Delete | `DELETE /instance/{id}`, `DELETE /disk/{id}` (retried while the disk is "mounted") |

Things to know:
- `cloudInit.custom = true` **replaces** Superphenix's cloud-init (which creates the `spx` user): the plugin creates the SSH user itself, with passwordless `sudo`.
- Deleting a VM does not delete its disks: the plugin creates them separately and deletes them explicitly.
- VM deletion is asynchronous: its disk stays "mounted" for a while.
- A VM that starts before the import of its boot disk has finished may boot from another disk: the plugin waits for `Succeeded`.

## Installing from an ISO
Tested with Ubuntu Server 26.04 (2026-11-08).
- Disks: a blank disk (`source.type = "blank"`, lowercase) at `order 0` (virtio) and the ISO (`http`, `cdrom: true`, `sata` bus) at `order 1`. Wait for `Succeeded` on both before creating the VM.
- **EFI is required** (`advanced.firmware.bootloader.efi.enabled`): with BIOS (SeaBIOS) only the hard disk is tried and the CD-ROM is never booted. With EFI, the blank disk is not bootable so the ISO starts, then the installed disk boots on its own when the installer reboots: no need to eject the CD-ROM. There is no `bootOrder` in the API.
- `boot_command`: `GET .../instance/{id}/vnc` is a WebSocket (`Authorization` header) speaking RFB 003.008 ("None" security). The plugin only sends `KeyEvent`s (`internal/client/vnc.go`) through the SDK's VNC driver. A screenshot can be read from the Raw rectangles (throwaway tool, not kept).
- Answer file: the instance's `cloudInit.config` (a `cidata` disk read by the installer). It avoids Packer's HTTP server, which the VM cannot reach if a security group blocks the runner. `http_content` remains possible.
- Ubuntu 26.04, GRUB EFI: the line `linux /casper/vmlinuz  ---` is the 4th line of the editor; `autoinstall` must be inserted **before** `---` (`<end><left><left><left><left>`: there is one character after the dashes). Check with a screenshot on each new release.
- The ISO disk is detached (`unmount`) then deleted at the end of the build. It stays attached during provisioning.
- Export validated from an Ubuntu 26.04 ISO (2026-11-09, 22 min 41 s, 6 GB qcow2) and from Windows. A first attempt the same day ended with "Timeout waiting for SSH" (40 min) with no cause found; the identical replay succeeded (not reproduced, probably sensitivity to `boot_command` timing). Not tested: booting an exported qcow2, Secure Boot and TPM on Linux, `cd_files` on a runner reachable by Superphenix.

## qcow2 export — variant 3b
1. `stop` the Packer VM (the builder's), then wait for the `Stopped` state.
2. `unmount` the Packer disk, then delete the Packer VM (the disk survives).
3. Create the helper's boot disk (`http` cloud image) and wait for its import.
4. Create the helper VM: cloud-init (`qemu-utils`, ephemeral SSH key), boot disk + Packer disk as second volume (`disks[].eid`).
5. SSH to the helper's private IP: detect the exported disk (neither the helper's root nor the cloud-init disk, unless `disk_device` is set), `qemu-img convert -O qcow2`, then download through SSH stdout, resuming if the stream stalls for 60 s.
6. Delete the helper, then the disks. Everything is in `defer`s: it also runs on failure or cancellation (Ctrl-C closes the SSH connection and removes the partial file).

## Known limits
- Tested with one API version and one AZ (`fr01-als01`); API 0.10 changed `compute.memory` from an integer in GiB to a quantity string (`"4Gi"`; `storage` stays an integer in GiB). The plugin accepts `4Gi` or `4`, sends `"4Gi"`, and if the API answers 400 mentioning `memory` retries once with the bare integer and remembers it (`CreateInstance`). Only tested against the pre-0.10 API so far.
- The helper's SSH host key is generated by the plugin (ed25519), injected through cloud-init `ssh_keys` and pinned (`ssh.FixedHostKey`). Until cloud-init has installed it the dial fails on a mismatch and is retried. The private key travels in the helper's user data (API payload), acceptable for a VM that lives for one export.
- Deleting a disk can return 500 for up to several minutes after its VM was deleted: the plugin retries for 10 min.
- If `Ctrl-C` or a crash kills Packer during the build, resources already created may remain: the `user.superphenix.net/created-by:packer*` labels make them easy to find.

## Ideas
- Runner outside the network: expose the VM through EIP + DNAT + SG, for both the builder and the helper. Requires reading and restoring the EIP state (existing SNAT, DNAT); not done.
- Release: files prepared but inactive in `release/` (goreleaser, release and CI workflows, activation procedure in `release/README.md`); to enable once the tests are broader. Untested (neither goreleaser nor GitHub Actions available here).

## Windows

- A `http` disk import is done by a Superphenix pod that has **no route to the runner's private IP** (timeout, no SYN on the runner side). An ISO generated by the runner (`cd_files`/`cd_content`) therefore cannot be imported; the answer-file ISO is static and hosted (`cd_url`, `example/windows/answer-iso/make-answer-iso.sh`).
- The VMs reach the runner (security group 10.0.0.0/24 to `http_port`). The WinRM bootstrap is downloaded at first logon by PowerShell from `http_directory`.
- WinPE of the Server 2025 FR ISO: Shift+F10 opens `X:\sources>`; no curl, no certutil, no PowerShell; AZERTY keyboard (VNC sends US keycodes); ~10 min of loading (`<wait600s>`).
- `vm_type = "windows.2k25"` sets EFI, Secure Boot and TPM: without explicit `efi`/`secure_boot`/`tpm`, the builder does not send `advanced`. System disk on SATA (`disk_bus`), no virtio driver during installation.
- VNC console: one session at a time.
- Validated end to end: Server 2025 FR, 36 min 32 s (ISO + hosted answer-file `cd_url` on `iso-host`, WinRM via PowerShell bootstrap, hardening, cleanup). qcow2 export of a Windows VM validated on 2026-11-09: 43 min 19 s in total, 12.46 GB file (50 GiB virtual, 11.6 GiB used). The first attempt failed because the runner's `/home` had only 915 MB free: the plugin took the local write error for a network stall ("stalled" resumes then `gave up after 20 resumes`). Fixed: `localWriter` (`transfer.go`) stops immediately on a local write error, and `freeBytes` compares free space with the qcow2 size before the download (not implemented on Windows: check skipped). The check happens after the conversion: the qcow2 size is only known then.
- The answer file is static (`example/windows/answer-iso/`, generator and validator); everything that varies between builds comes from the runner's HTTP server. The test infrastructure IDs (`iso-host`, EIP, security groups) are in `docs/test-infrastructure.md`.
- User guides: `docs/windows.md` and `docs/linux.md`.
