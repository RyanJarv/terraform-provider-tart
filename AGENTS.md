# Workspace instructions

This project is pre-release. Backward compatibility is optional.

Own the design. Look for simpler ways to meet the user's goal and fix technical debt as you go. Finish and verify the work before calling it complete. If blocked, explain why, what remains, and the next step.

## When to proceed and when to ask

**Proceed independently** when the intended behavior is clear.

- Remove obsolete, unnecessary, or duplicate behavior, code, tests, and docs.
- Simplify logic while preserving how it handles important behavior.

**Ask first** about unrequested behavior changes or tool and dependency changes. Explain the consequence and recommend an option.

- Making a setting required or changing its default.
- Replacing data users expect to update in place.
- Removing an ownership check or a required opt-in for a destructive action.
- Adding, downloading, installing, or changing a host or guest tool or dependency, including transitive dependencies, temporary installs or caches, plugin setup, and manifest or lockfile edits. Get explicit user approval with the exact name, pinned version, source URI, reason, and install or cache location.

## Improving how we work

- When a request conflicts with a project rule, consider whether the rule itself is wrong.
- When you spot an improvement to project guidance or workflow, use `$improvement-proposal-template` to ask before adopting it.

## Purpose and design

Use Terraform to plan changes to local Tart VMs on Apple silicon and reproduce them from stopped base images. See the [provider design guide](PROVIDER_DESIGN.md) for Terraform conventions and their application here.

- Keep schema, state, and planning in the resource and data source. Route Tart CLI and store access through the client. Data sources only read.
- Use Terraform's lifecycle directly. After apply, unchanged configuration and VMs should yield an empty plan.
- Look up VMs by name in the configured store, serialize provider writes per store, and read back results before recording state.
- Use Terraform’s lifecycle controls for replacement and destroy. See the [VM resource lifecycle](docs/resources/vm.md#lifecycle).
