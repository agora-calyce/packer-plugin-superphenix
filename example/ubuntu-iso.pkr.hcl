# Installation d'Ubuntu Server depuis l'ISO (autoinstall via cloud-init), puis export qcow2.
# Testé avec ubuntu-26.04-live-server-amd64.iso : voir docs/builders/superphenix.mdx.
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
variable "subnet_eid" { type = string }
variable "token" {
  type      = string
  sensitive = true
}
variable "ssh_password" {
  type      = string
  sensitive = true
}
variable "ssh_password_hash" { # mkpasswd -m sha-512
  type      = string
  sensitive = true
}

locals {
  iso = "https://releases.ubuntu.com/26.04/ubuntu-26.04-live-server-amd64.iso"
}

source "superphenix" "ubuntu-iso" {
  api_url       = var.api_url
  org_id        = var.org_id
  az            = var.az
  project_id    = var.project_id
  token         = var.token
  subnet_eid    = var.subnet_eid
  storage_class = "block-standard"

  iso_url   = local.iso
  cpu       = 2
  memory    = "4Gi"
  disk_size = "20Gi"

  # Réponse de l'installeur, lue sur le disque cloud-init de l'instance (aucun accès réseau nécessaire).
  user_data = <<-EOT
    #cloud-config
    autoinstall:
      version: 1
      locale: en_US.UTF-8
      keyboard: {layout: us}
      storage: {layout: {name: direct}}
      ssh: {install-server: true, allow-pw: true}
      identity:
        hostname: ubuntu-iso
        username: adminsys
        password: "${var.ssh_password_hash}"
      user-data:
        users:
          - name: adminsys
            sudo: ALL=(ALL) NOPASSWD:ALL
            shell: /bin/bash
            lock-passwd: false
  EOT

  boot_wait = "2s"
  boot_command = [
    "<spacebar><wait><spacebar><wait><spacebar><wait>",
    "e<wait><down><down><down><end><left><left><left><left>", # avant le --- de la ligne linux
    "autoinstall ",
    "<wait><f10>",
  ]

  communicator = "ssh"
  ssh_username = "adminsys"
  ssh_password = var.ssh_password
  ssh_timeout  = "40m"
}

build {
  sources = ["source.superphenix.ubuntu-iso"]

  provisioner "shell" {
    inline = ["cat /etc/os-release | head -2"]
  }

  # Non testé sur une VM issue d'une ISO (testé depuis une cloud image).
  post-processor "superphenix-export" {
    api_url          = var.api_url
    org_id           = var.org_id
    az               = var.az
    project_id       = var.project_id
    token            = var.token
    subnet_eid       = var.subnet_eid
    storage_class    = "block-standard"
    helper_image_url = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
    output_path      = "output/ubuntu-iso.qcow2"
  }
}
