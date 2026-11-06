The Superphenix plugin builds machine images on a [Superphenix](https://github.com/super-phenix/superphenix)
hypervisor (KubeVirt based) and exports them as `qcow2` files. It is designed to run in a CI job, for
example on a GitLab runner, and produce the image as a CI artifact.

### Installation

To install this plugin, copy and paste this code into your Packer configuration, then run [`packer init`](https://www.packer.io/docs/commands/init).

```hcl
packer {
  required_plugins {
    superphenix = {
      version = ">= 0.1.0"
      source  = "github.com/agora-calyce/superphenix"
    }
  }
}
```

Alternatively, you can use `packer plugins install` to manage installation of this plugin.

```sh
$ packer plugins install github.com/agora-calyce/superphenix
```

### Components

#### Builders

- [superphenix](/packer/integrations/agora-calyce/superphenix/latest/components/builder/superphenix) - Creates a VM
  from a cloud image, provisions it over SSH, then stops it.

#### Post-processors

- [superphenix-export](/packer/integrations/agora-calyce/superphenix/latest/components/post-processor/export) -
  Converts the disk of the built VM to `qcow2` and downloads it to the machine running Packer.

### Authentication

Both components call the Superphenix API with a Bearer token. Pass it through a sensitive variable, for example
`PKR_VAR_token` set as a masked CI variable, and never write it in a template.
