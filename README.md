# Terraform provider for Tart

Note: wip not reviewed, don’t use

A Terraform provider for managing local Tart VMs on Apple silicon macOS. It invokes the installed Tart CLI directly and targets one explicitly configured store. Tested with Tart 2.41.1.

## Build and use

After Go and this repository's pinned dependencies are available, build the provider into a directory of your choice:

```sh
go build -mod=readonly \
  -o /absolute/path/to/bin/terraform-provider-tart .
```

For local use, select a dedicated Terraform CLI configuration with `TF_CLI_CONFIG_FILE`. Development overrides resolve the provider from the build directory without `terraform init` for a configuration using only this provider. [Terraform development overrides](https://developer.hashicorp.com/terraform/plugin/debugging)

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/ryanjarv/tart" = "/absolute/path/to/bin"
  }
}
```

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
  # tart_path defaults to /opt/homebrew/bin/tart
}

data "tart_vm" "base" {
  name = "ubuntu-base"
}

resource "tart_vm" "worker" {
  name         = "ubuntu-worker"
  source_image = data.tart_vm.base.name
  cpu_count    = 4
  memory_mib   = 4096

  lifecycle {
    prevent_destroy = true
  }
}
```

Both names refer to the selected Tart store. The resource clones a stopped local base and sets the CPU and memory. The data source reports observed runtime status. Virtual disk size is reported in Tart's GB units, and memory is in MiB.

## Existing VMs

To adopt an existing stopped VM, configure a resource with its exact current name, CPU count, and memory. Omit `source_image` and `disposable`, then run `terraform import tart_vm.<resource> <local-vm-name>`. Review the no-change plan before allowing writes. Import records the actual VM directory identity and does not infer image provenance. If the VM is missing or has been replaced under the same name, refresh returns an error and preserves Terraform state. Use a Terraform `removed` block with `destroy = false` when deliberately relinquishing management. [Terraform resource import](https://developer.hashicorp.com/terraform/plugin/framework/resources/import)

## Write boundaries

- The provider serializes writes from its own processes in the selected store. Tart CLI operations run separately. Avoid starting, renaming, replacing, or resizing a VM from the CLI during a provider write.
- Cloning rejects an occupied destination and disables automatic pruning. A partially completed clone is left available for inspection. Use import or pick a new name after checking the destination.
- CPU and memory changes require a stopped VM and are verified from Tart after writing. Changing the name, source image, or disposal policy requires choosing a separate VM.
- Deletion is disabled for persistent or imported VMs. To opt into deletion for a disposable clone, use a separate scratch store containing a regular file named `.terraform-tart-scratch` whose contents are exactly `terraform-provider-tart disposable store v1` followed by a newline, then set `disposable = true` at creation. Delete checks scratch-store membership, the provider's ownership marker, VM identity, disk identity, and stopped status at apply. Use Terraform's `prevent_destroy` for an additional plan-level guard on persistent instances. [Terraform lifecycle](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle)
- Start, stop, backup, recovery, and disk resizing remain Tart CLI operations. Guest accounts, packages, and services belong to the image or guest provisioning workflow.

## Tests

`go test -mod=readonly ./...` uses a local fake Tart executable for JSON parsing, failure classification, path handling, and identity and disk protection. Run `sh tests/lifecycle.sh /absolute/terraform /absolute/tart /absolute/provider-bin-dir /absolute/ubuntu-arm64.iso` on Apple silicon for the full lifecycle. Use a verified ARM64 installer. The test attaches it read-only to keep the disposable VM running during the stopped-only resize check. The test creates an isolated Tart store and leaves its path in the output for inspection.

## Reference and releases

[Provider settings](docs/index.md) · [VM resource](docs/resources/vm.md) · [VM data source](docs/data-sources/vm.md) · [Release procedure](RELEASING.md) · [MIT license](LICENSE)
