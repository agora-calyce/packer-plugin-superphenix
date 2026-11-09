# Windows Server 2025 depuis une ISO, WinRM, puis export.
# Superphenix importe les ISO depuis une URL qu'il atteint (pas depuis le runner en IP privée) : l'ISO d'answer file
# est donc statique et hébergée (make-answer-iso.sh), et seul le bootstrap WinRM vient du serveur HTTP de Packer.
# Les VMs doivent joindre le runner sur http_port (groupe de sécurité : 10.0.0.0/24 -> 8080).
# Le mot de passe win_password doit être celui passé à make-answer-iso.sh (PASSWORD).

packer {
  required_plugins {
    superphenix = {
      version = ">= 0.1.0"
      source  = "github.com/agora-calyce/superphenix"
    }
  }
}

variable "token" {
  type      = string
  sensitive = true
}
variable "win_password" {
  type      = string
  sensitive = true
}
variable "answer_iso_url" {
  type        = string
  description = "URL de l'ISO produite par answer-iso/make-answer-iso.sh"
}
variable "api_url" { default = "https://superphenix.example.com/api/" }
variable "org_id" {}
variable "az" {}
variable "project_id" {}
variable "subnet_eid" {}
variable "storage_class" { default = "block-standard" }
variable "win_username" { default = "Administrateur" }

source "superphenix" "win2025" {
  api_url       = var.api_url
  org_id        = var.org_id
  az            = var.az
  project_id    = var.project_id
  token         = var.token
  subnet_eid    = var.subnet_eid
  storage_class = var.storage_class

  vm_type       = "windows.2k25" # fixe EFI, Secure Boot et TPM
  disk_bus      = "sata"         # pas de pilote virtio pendant l'installation
  iso_url       = "https://example.com/windows-server-2025.iso"
  iso_disk_size = "8Gi"
  cpu           = 4
  memory        = "8Gi"
  disk_size     = "50Gi"

  http_port_min = 8080 # ouvert par le groupe de sécurité runner-packer-http
  http_port_max = 8080

  # ISO d'answer file statique, générée par answer-iso/make-answer-iso.sh et hébergée une fois.
  # Windows en récupère le bootstrap WinRM sur le serveur HTTP de Packer (http_directory).
  cd_url         = var.answer_iso_url
  http_directory = "${path.root}/scripts"

  # "Press any key to boot from CD" : le timing varie, on répète. Les appuis en trop arrivent sur le menu du
  # Windows Boot Manager et y suspendent le compte à rebours : le <enter> final lance "Windows Setup".
  boot_wait    = "5s"
  boot_command = ["<spacebar><wait3><spacebar><wait3><spacebar><wait3><spacebar><wait3><enter>"]

  communicator   = "winrm"
  winrm_username = var.win_username
  winrm_password = var.win_password
  winrm_use_ssl  = false
  winrm_insecure = true
  winrm_timeout  = "2h" # install ~30 min en conditions normales, large marge
}

build {
  sources = ["source.superphenix.win2025"]

  provisioner "powershell" {
    elevated_user     = var.win_username
    elevated_password = var.win_password
    inline = [
      "$sizeMax = (Get-PartitionSupportedSize -DriveLetter C).SizeMax",
      "Resize-Partition -DriveLetter C -Size $sizeMax"
    ]
  }

  # Chemin EFI de repli : la NVRAM n'est pas toujours conservée, le BootOrder est alors perdu au redémarrage.
  provisioner "powershell" {
    elevated_user     = var.win_username
    elevated_password = var.win_password
    inline = [
      "$esp = Get-Partition | Where-Object { $_.GptType -eq '{c12a7328-f81f-11d2-ba4b-00a0c93ec93b}' }",
      "$esp | Add-PartitionAccessPath -AccessPath 'S:'",
      "New-Item -ItemType Directory -Force -Path 'S:\\EFI\\Boot' | Out-Null",
      "Copy-Item 'C:\\Windows\\Boot\\EFI\\bootmgfw.efi' 'S:\\EFI\\Boot\\bootx64.efi' -Force",
      "$esp | Remove-PartitionAccessPath -AccessPath 'S:'"
    ]
  }

  provisioner "powershell" {
    elevated_user     = var.win_username
    elevated_password = var.win_password
    execution_policy  = "remotesigned"
    scripts = [
      "${path.root}/scripts/windows-preparation.ps1",
      "${path.root}/scripts/windows-cleanup.ps1",
    ]
  }

  # Export qcow2 : une VM auxiliaire convertit le disque et l'envoie au runner.
  # Le disque Windows contient un système EFI : démarrer le qcow2 avec un firmware UEFI.
  post-processor "superphenix-export" {
    api_url          = var.api_url
    org_id           = var.org_id
    az               = var.az
    project_id       = var.project_id
    token            = var.token
    subnet_eid       = var.subnet_eid
    storage_class    = var.storage_class
    helper_image_url = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
    helper_disk_size = "40Gi" # doit contenir le qcow2 converti
    output_path      = "output/windows-server-2025.qcow2"
  }
}
