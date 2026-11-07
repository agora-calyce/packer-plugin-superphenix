# Changelog

## Unreleased

### Breaking changes

- `superphenix-export`: removed `eip_eid` and `runner_cidr`. EIP exposure is no
  longer supported. Packer must now run inside the Superphenix subnet (or behind
  a VPN) to reach the helper VM's private IP, as it already does for the
  builder. Remove these two keys from existing templates; Packer reports them
  as unknown arguments.
