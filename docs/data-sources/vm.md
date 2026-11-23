# tart_vm Data Source

Reads metadata for a VM in the provider's configured Tart store.

```hcl
data "tart_vm" "base" {
  name = "ubuntu-base"
}
```

## Arguments

- `name` (required) is the VM's local name.

## Read-only attributes

- `id` is the VM name in the configured store.
- `os` is the guest platform.
- `cpu_count` is the configured number of vCPUs.
- `memory_mib` is the configured memory in MiB.
- `disk_size_gb` is the virtual disk capacity reported by Tart.
- `status` is the runtime status observed at refresh.
