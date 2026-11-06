
# Superphenix Export Post-Processor

Type: `superphenix-export`
Artifact BuilderId: `agora-calyce.superphenix.export`

The Superphenix API has no endpoint to download a disk. The `superphenix-export` post-processor therefore
exports the disk of a VM built by the [superphenix builder](/packer/integrations/agora-calyce/superphenix/latest/components/builder/superphenix)
through a short-lived helper VM:

1. The disk of the built VM is detached and the built VM is deleted.
2. A helper VM is created from `helper_image_url`, with the detached disk attached as a second disk.
   Its cloud-init installs `qemu-utils` and authorizes a temporary SSH key.
3. The post-processor waits for the helper's boot disk to be imported before starting the helper, then
   connects to its private IP, as the builder does.
4. Over SSH, the helper runs `qemu-img convert -O qcow2` on the exported disk (detected automatically, or
   `disk_device`), and the file is downloaded to `output_path`. An interrupted download is resumed.
5. The helper and its disks are removed, including when a step fails or the build is cancelled.

The built VM, the helper and the exported disk are all deleted at the end of the post-processor. Only the
`qcow2` file remains.

~> **Note:** the machine running Packer must reach the private IP of the helper, so it must run inside the
Superphenix network (a VM in the subnet, or behind a VPN). SSH host keys of the helper are not verified, since
the helper is created for the occasion and its key is not known in advance.

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

- `storage_class` (string) - Storage class of the helper's boot disk, for example `block-standard`.

- `subnet_eid` (string) - Subnet the helper is attached to. The machine running Packer must be able
  to reach the helper's private IP address, as it does for the builder.

<!-- End of code generated from the comments of the Config struct in postprocessor/postprocessor.go; -->


### Optional:

<!-- Code generated from the comments of the Config struct in postprocessor/postprocessor.go; DO NOT EDIT MANUALLY -->

- `helper_cpu` (int) - vCPUs of the helper VM. Defaults to `2`.

- `helper_memory` (string) - Memory of the helper VM, a whole number of GiB such as `2Gi`. Defaults to `2Gi`.

- `helper_disk_size` (string) - Size of the helper's boot disk, a whole number of GiB such as `30Gi`. It
  also holds the converted qcow2, so it must be larger than the image
  produced. Defaults to `30Gi`.

- `helper_ssh_username` (string) - SSH user of the helper image. Defaults to `spx`.

- `disk_device` (string) - Device of the exported disk inside the helper, for example `/dev/vdb`.
  Detected automatically by default: the disk that is neither the
  helper's boot disk nor the cloud-init disk.

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
  storage_class    = "block-standard"
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

Set `PKR_VAR_token` as a masked CI/CD variable. The GitLab runner must be able to reach the private IPs of the
subnet (runner hosted in the subnet, or VPN). Use `resource_group` to run one build at a time.
