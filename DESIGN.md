# Packer plugin for Superphenix — design

## Scope
- Go plugin (`packer-plugin-sdk`): **builder** + **post-processor**.
- Builder: creates the VM from a cloud image (`http` source), provisions through the SSH communicator. No ISO, no WinRM.
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
| Create VM / helper | `POST /instance` (`network` required, `compute.memory` integer in GiB, labels `user.superphenix.net/<key>:<value>`) |
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

## qcow2 export — variant 3b
1. `stop` the Packer VM (the builder's), then wait for the `Stopped` state.
2. `unmount` the Packer disk, then delete the Packer VM (the disk survives).
3. Create the helper's boot disk (`http` cloud image) and wait for its import.
4. Create the helper VM: cloud-init (`qemu-utils`, ephemeral SSH key), boot disk + Packer disk as second volume (`disks[].eid`).
5. SSH to the helper's private IP: detect the exported disk (neither the helper's root nor the cloud-init disk, unless `disk_device` is set), `qemu-img convert -O qcow2`, then download through SSH stdout, resuming if the stream stalls for 60 s.
6. Delete the helper, then the disks. Everything is in `defer`s: it also runs on failure or cancellation (Ctrl-C closes the SSH connection and removes the partial file).

## Known limits
- Tested with one API version and one AZ (`fr01-als01`); the deployed version expects `memory` as an integer in GiB, whereas recent `main` code expects a quantity (`"4Gi"`): to follow when the API is updated.
- The helper's SSH host key is not verified (ephemeral VM, key unknown in advance).
- If `Ctrl-C` or a crash kills Packer during the build, resources already created may remain: the `user.superphenix.net/created-by:packer*` labels make them easy to find.

## Ideas
- Runner outside the network: expose the VM through EIP + DNAT + SG, for both the builder and the helper. Requires reading and restoring the EIP state (existing SNAT, DNAT); not done.
- Release: goreleaser (`linux/amd64`, `linux/arm64` binaries, checksums) once the tests are broader.
