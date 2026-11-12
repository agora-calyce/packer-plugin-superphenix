# Ubuntu from a cloud image, provisioned over SSH, then exported to qcow2.
packer {
  required_plugins {
    superphenix = {
      source  = "github.com/agora-calyce/superphenix"
      version = ">= 0.1.0"
    }
  }
}

variable "api_url" { type = string }
variable "org_id" { type = string }
variable "az" { type = string }
variable "project_id" { type = string }
variable "subnet_eid" { type = string }
variable "token" {
  type      = string
  sensitive = true # injected by the masked CI variable PKR_VAR_token
}

locals {
  image = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
  # Not the build image: two disks with the same labels/UUIDs make the helper mount the wrong one.
  helper_image = "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2"
}

source "superphenix" "ubuntu" {
  api_url    = var.api_url
  org_id     = var.org_id
  az         = var.az
  project_id = var.project_id
  token      = var.token

  source_url    = local.image
  cpu           = 2
  memory        = "4Gi"
  disk_size     = "20Gi"
  subnet_eid    = var.subnet_eid
  storage_class = "block-standard"

  communicator = "ssh"
  ssh_username = "spx"
}

build {
  sources = ["source.superphenix.ubuntu"]

  provisioner "shell" {
    inline = ["sudo apt-get update", "sudo apt-get install -y nginx"]
  }

  post-processor "superphenix-export" {
    api_url    = var.api_url
    org_id     = var.org_id
    az         = var.az
    project_id = var.project_id
    token      = var.token

    output_path      = "output/ubuntu.qcow2"
    helper_image_url = local.helper_image
    subnet_eid       = var.subnet_eid
    storage_class    = "block-standard"
  }
}
