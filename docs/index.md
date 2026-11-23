# Tart Provider

Manage local [Tart](https://tart.run/) VMs on Apple silicon macOS. The provider
uses the Tart CLI on the machine running Terraform. It clones local images,
manages CPU and memory, and reads VM metadata from an existing Tart store.

## Example usage

With a stopped `ubuntu-base` VM in the default Tart store:

```hcl
terraform {
  required_providers {
    tart = {
      source = "ryanjarv/tart"
    }
  }
}

resource "tart_vm" "worker" {
  name         = "ubuntu-worker"
  source_image = "ubuntu-base"
  cpu_count    = 4
  memory_mib   = 4096
}
```

## Argument reference

- `tart_home` (optional) is the absolute path to an existing Tart store. It defaults to `TART_HOME` when set and `~/.tart` otherwise.
- `tart_path` (optional) is the absolute path to the Tart CLI. When omitted, the provider looks up `tart` on Terraform’s `PATH`.
