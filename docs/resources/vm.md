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
- `cpu_count` and `memory_mib` (required) are positive integers. The VM must be stopped to resize it.
- `disposable` defaults to `false`. Use `true` at creation to permit deletion in a separate, marked scratch store. Write `terraform-provider-tart disposable store v1` followed by a newline to a regular file named `.terraform-tart-scratch` in that store. The provider also checks ownership, VM and disk identities, and stopped status before deleting a clone.

Name, base, and disposal policy are immutable. For routine start and stop, use Tart.

## Read-only attributes

- `id` is the provider's VM identity.
- `os` is the guest platform.
- `disk_size_gb` is the virtual disk capacity reported by Tart.

## Import

Declare the VM with its current CPU and memory and omit `source_image` and
`disposable`. Stop the VM, import it by name, and review the resulting plan.

```sh
terraform import tart_vm.worker ubuntu-worker
```
