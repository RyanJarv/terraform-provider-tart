# Tart Provider

Manage local [Tart](https://tart.run/) VMs on Apple silicon macOS. The provider
uses the Tart CLI on the machine running Terraform. It clones local images,
manages CPU and memory, and reads VM metadata from an existing Tart store.

## Example usage

```hcl
terraform {
  required_providers {
    tart = {
      source = "ryanjarv/tart"
    }
  }
}

provider "tart" {
  tart_home = "/absolute/path/to/tart-store"
}
```

## Argument reference

- `tart_home` (required) is the absolute path to an existing store owned by the Terraform user.
- `tart_path` (optional) is the absolute path to the Tart CLI. It defaults to `/opt/homebrew/bin/tart`.
