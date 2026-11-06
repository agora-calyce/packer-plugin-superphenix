
# Superphenix Export Post-Processor

Type: `superphenix-export`
Artifact BuilderId: `agora-calyce.superphenix.export`

The Superphenix API has no endpoint to download a disk. The `superphenix-export` post-processor therefore
exports the disk of a VM built by the [superphenix builder](/packer/integrations/agora-calyce/superphenix/latest/components/builder/superphenix)
through a short-lived helper VM:

1. The disk of the built VM is detached and the built VM is deleted.
2. A helper VM is created from `helper_image_url`, with the detached disk attached as a second disk.
   Its cloud-init installs `qemu-utils` and authorizes a temporary SSH key.
3. If `eip_eid` is set, a security group allowing only SSH from `runner_cidr` is created and the EIP is
   associated with the helper. Otherwise the helper's private IP is used.
4. Over SSH, the helper runs `qemu-img convert -f raw -O qcow2` on the disk, and the file is downloaded to
   `output_path`.
5. The helper, its disk, the security group and the EIP association are removed, including when a step fails.

The built VM, the helper and the exported disk are all deleted at the end of the post-processor. Only the
`qcow2` file remains.

~> **Note:** SSH host keys of the helper are not verified, since the helper is created for the occasion and its
key is not known in advance. Limit access with `runner_cidr`.

## Configuration Reference

### Required:

<!-- Code generated from the comments of the Config struct in postprocessor/postprocessor.go; DO NOT EDIT MANUALLY -->

- `output_path` (string) - Path of the qcow2 file written on the machine running Packer.

- `api_url` (string) - Base URL of the Superphenix API.

- `org_id` (string) - ID of the organization that owns the project.

- `az` (string) - Availability zone, the same as the builder's.

- `project_id` (string) - ID of the project, the same as the builder's.

- `token` (string) - API token. Pass it through a sensitive variable.

- `helper_image_url` (string) - URL of a Linux cloud image used for the helper VM. It must provide
  cloud-init and allow installing `qemu-utils` from its package manager.

<!-- End of code generated from the comments of the Config struct in postprocessor/postprocessor.go; -->


### Optional:

<!-- Code generated from the comments of the Config struct in postprocessor/postprocessor.go; DO NOT EDIT MANUALLY -->

- `helper_cpu` (int) - vCPUs of the helper VM. Defaults to `2`.

- `helper_memory` (string) - Memory of the helper VM. Defaults to `2Gi`.

- `helper_disk_size` (string) - Size of the helper's boot disk. It also holds the converted qcow2, so it
  must be larger than the image produced. Defaults to `30Gi`.

- `helper_ssh_username` (string) - SSH user of the helper image. Defaults to `spx`.

- `disk_device` (string) - Device of the exported disk inside the helper. Defaults to `/dev/vdb`.

- `storage_class` (string) - Storage class of the helper's boot disk. Defaults to the AZ default.

- `subnet_eid` (string) - Subnet the helper is attached to. Required with `eip_eid`.

- `eip_eid` (string) - Effective ID of a pre-allocated EIP. When set, the helper is exposed
  through this EIP and a security group that only allows SSH from
  `runner_cidr`. When empty, the helper's private IP is used, which
  requires the machine running Packer to be inside the network.

- `runner_cidr` (string) - CIDR allowed to reach the helper over SSH, typically the outgoing
  address of the CI runner as a `/32`. Required with `eip_eid`.

<!-- End of code generated from the comments of the Config struct in postprocessor/postprocessor.go; -->


## Example

```hcl
post-processor "superphenix-export" {
  api_url    = var.api_url
  org_id     = var.org_id
  az         = var.az
  project_id = var.project_id
  token      = var.token

  output_path      = "output/ubuntu.qcow2"
  helper_image_url = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
  subnet_eid       = var.subnet_eid
  eip_eid          = var.eip_eid
  runner_cidr      = "198.51.100.10/32"
}
```

## Using it in GitLab CI

Run Packer in a job and keep the file as a job artifact:

```yaml
build-image:
  image:
    name: hashicorp/packer:latest
    entrypoint: [""]
  script:
    - mkdir -p output
    - packer init .
    - packer build -color=false .
  artifacts:
    paths: [output/ubuntu.qcow2]
  resource_group: superphenix-packer
```

Set `PKR_VAR_token` as a masked CI/CD variable. Use `resource_group` so that only one build uses the EIP at a
time.
