packer {
  required_plugins {
    superphenix = {
      source  = "github.com/agora-calyce/superphenix"
      version = ">= 0.1.0"
    }
  }
}

variable "api_url"    { type = string }
variable "org_id"     { type = string }
variable "az"         { type = string }
variable "project_id" { type = string }
variable "token" {
  type      = string
  sensitive = true # injecté par la variable CI masquée PKR_VAR_token
}
variable "subnet_eid"  { type = string }
variable "eip_eid"     { type = string } # EIP pré-allouée pour le helper d'export
variable "runner_cidr" { type = string } # IP sortante du runner GitLab, ex. 198.51.100.10/32

locals {
  image = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
}

source "superphenix" "ubuntu" {
  api_url    = var.api_url
  org_id     = var.org_id
  az         = var.az
  project_id = var.project_id
  token      = var.token

  source_url = local.image
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

  post-processor "superphenix-export" {
    api_url    = var.api_url
    org_id     = var.org_id
    az         = var.az
    project_id = var.project_id
    token      = var.token

    output_path      = "output/ubuntu.qcow2"
    helper_image_url = local.image
    subnet_eid       = var.subnet_eid
    storage_class    = "block-standard"
    eip_eid          = var.eip_eid
    runner_cidr      = var.runner_cidr
  }
}
