# Metadata read by the Packer integrations library (developer.hashicorp.com/packer/integrations).
integration {
  name        = "Superphenix"
  description = "Build images on a Superphenix hypervisor and export them as qcow2."
  identifier  = "packer/agora-calyce/superphenix"
  docs {
    process_docs    = true
    readme_location = "./README.md"
    external_url    = "https://github.com/agora-calyce/packer-plugin-superphenix"
  }
  license {
    type = "MPL-2.0"
    url  = "https://github.com/agora-calyce/packer-plugin-superphenix/blob/main/LICENSE"
  }
  component {
    type = "builder"
    name = "Superphenix"
    slug = "superphenix"
  }
  component {
    type = "post-processor"
    name = "Superphenix Export"
    slug = "export"
  }
}
