# Terraform provider for Tart

Note: wip not reviewed, don’t use

Review Note: This doesn't actually make sense. All this does is use the tart CLI to make changes to a local config file. Better just to store the config directly in a repo and use the CLI to make changes.

A Terraform provider for managing local Tart VMs on Apple silicon macOS. It invokes the installed Tart CLI directly. Tested with Tart 2.41.1.

## Build and use

Terraform launches the [plugin executable](provider/main.go), which serves the [provider implementation](provider/internal/provider/). The [Tart CLI client](tart/) is a separate Go module. From the repository root, build the provider with the pinned dependencies.

```sh
go -C provider build -mod=readonly \
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

With Tart on `PATH` and a stopped local VM named `ubuntu-base`, this configuration creates a clone:

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

The provider uses `TART_HOME` when set and `~/.tart` otherwise. Set `tart_home` to an absolute path to select a specific store, such as a separate disposable test store. The store must already exist. The provider finds `tart` on Terraform’s `PATH`. Set `tart_path` to select a specific executable by absolute path.

## Existing VMs

Configure the resource with the VM's current name, CPU count, and memory. Omit `source_image` and run `terraform import tart_vm.<resource> <local-vm-name>`. Review the plan. The resource ID is the VM name within the configured store. If you recreate a VM under that name, Terraform manages the new VM. Use a Terraform `removed` block with `destroy = false` when deliberately relinquishing management. [Terraform resource import](https://developer.hashicorp.com/terraform/plugin/framework/resources/import)

## Write boundaries

- The provider serializes writes from its own processes in the selected store. Tart CLI operations run separately. Avoid starting, renaming, replacing, or resizing a VM from the CLI during a provider write.
- Cloning rejects an occupied destination and disables automatic pruning. A partially completed clone is left available for inspection. Use import or pick a new name after checking the destination.
- Terraform updates and verifies the saved CPU and memory settings even while the VM is running. Shut down the guest and start it again with Tart when you want the new settings to take effect. Changing the name or source image plans replacement.
- Destroy and replacement call `tart delete` on the named VM, including an imported VM. Set `lifecycle { prevent_destroy = true }` on persistent resources to reject destructive plans while the rule is configured. [Terraform lifecycle](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle)
- Start, stop, backup, recovery, and disk resizing remain Tart CLI operations. Guest accounts, packages, and services belong to the image or guest provisioning workflow.

## Go client

Import `github.com/ryanjarv/terraform-provider-tart/tart` to manage local VMs from Go. The [standalone module](tart/) uses only the standard library. Its four operations are `Get`, `Clone`, `Resize`, and `Delete`.

```go
func cloneWorker(ctx context.Context) (tart.VM, error) {
    client, err := tart.NewClient(tart.Config{}) // Uses tart on PATH and the default store.
    if err != nil {
        return tart.VM{}, err
    }
    if err := client.Clone(ctx, "ubuntu-base", "worker"); err != nil {
        return tart.VM{}, err
    }
    return client.Resize(ctx, "worker", tart.Size{CPUCount: 4, MemoryMiB: 4096})
}
```

## Tests

Run both modules’ tests from the repository root.

```sh
go -C provider test -mod=readonly ./...
go -C tart test -mod=readonly ./...
```

Run `sh tests/lifecycle.sh /absolute/provider-bin-dir /absolute/ubuntu-arm64.iso` on Apple silicon for the full lifecycle. Use a verified ARM64 installer. The test attaches it read-only to keep the disposable VM running while testing CPU and memory changes. The test creates an isolated Tart store and leaves its path in the output for inspection.

## Reference and releases

[Provider settings](docs/index.md) · [VM resource](docs/resources/vm.md) · [VM data source](docs/data-sources/vm.md) · [Release procedure](RELEASING.md) · [MIT license](LICENSE)
