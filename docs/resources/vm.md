# tart_vm Resource

Clones a stopped local base and manages the new VM's CPU and memory.

```hcl
resource "tart_vm" "worker" {
  name         = "ubuntu-worker"
  source_image = "ubuntu-base"
  cpu_count    = 4
  memory_mib   = 4096
}
```

## Arguments

- `name` (required) is the VM's local name.
- `source_image` is the local stopped base. Supply it for a new clone and omit it for import.
- `cpu_count` and `memory_mib` (required) are positive integers.

## Lifecycle

- **Resize.** Terraform updates the saved CPU and memory settings in place, including while the VM is running. Shut down the guest and start it again with Tart to use the new settings.
- **Replace.** Changing `name` or `source_image` requires replacement.
- **Delete.** Replacement and destroy delete the VM currently at that name in the configured store, including imported VMs. Use Terraform’s `prevent_destroy` lifecycle rule to protect a configured resource.
- **Refresh.** If a VM is missing, Terraform can recreate it from its configured base. If another VM occupies its name, Terraform reads that VM and reconciles its settings.

When upgrading to 0.2.0, remove any `disposable` or `allow_deletion` resource argument. Terraform migrates the stored ID to the VM name. To retain deletion protection, add `prevent_destroy` before planning the upgrade.

## Read-only attributes

- `id` is the VM name in the configured store.
- `os` is the guest platform.
- `disk_size_gb` is the virtual disk capacity reported by Tart.

## Import

Declare the VM with its current CPU and memory and omit `source_image`. Import it by name and review the resulting plan.

```sh
terraform import tart_vm.worker ubuntu-worker
```
