package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ryanjarv/terraform-provider-tart/tart"
)

type vmResource struct {
	client *tart.Client
}

type vmModel struct {
	Name   types.String  `tfsdk:"name"`
	Source types.String  `tfsdk:"source_image"`
	ID     types.String  `tfsdk:"id"`
	OS     types.String  `tfsdk:"os"`
	CPU    types.Int64   `tfsdk:"cpu_count"`
	Memory types.Int64   `tfsdk:"memory_mib"`
	Disk   types.Float64 `tfsdk:"disk_size_gb"`
}

var (
	_ resource.ResourceWithConfigure   = (*vmResource)(nil)
	_ resource.ResourceWithImportState = (*vmResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*vmResource)(nil)
)

func (*vmResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm"
}

func (*vmResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Version: 2, Attributes: map[string]schema.Attribute{
		"name":         schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Description: "Local VM name. Changing the name requires replacement."},
		"source_image": schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Description: "Local stopped base image. Required for creation; omit for import."},
		"id":           schema.StringAttribute{Computed: true, Description: "VM name in the configured Tart store."},
		"os":           schema.StringAttribute{Computed: true},
		"cpu_count":    schema.Int64Attribute{Required: true},
		"memory_mib":   schema.Int64Attribute{Required: true},
		"disk_size_gb": schema.Float64Attribute{Computed: true, Description: "Virtual disk capacity as reported by Tart."},
	}}
}

// Both prior schemas stored a filesystem fingerprint and a deletion permission.
func (*vmResource) UpgradeState(context.Context) map[int64]resource.StateUpgrader {
	upgrader := resource.StateUpgrader{StateUpgrader: upgradeVMState}
	return map[int64]resource.StateUpgrader{0: upgrader, 1: upgrader}
}

func upgradeVMState(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(req.RawState.JSON, &attrs); err != nil || attrs == nil {
		resp.Diagnostics.AddError("Cannot upgrade Tart VM state", "Expected a VM object.")
		return
	}
	attrs["id"] = attrs["name"]
	delete(attrs, "disposable")
	delete(attrs, "allow_deletion")
	raw := *req.RawState
	var err error
	raw.JSON, err = json.Marshal(attrs)
	if err == nil {
		resp.State.Raw, err = raw.Unmarshal(resp.State.Schema.Type().TerraformType(ctx))
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot upgrade Tart VM state", err.Error())
	}
}

func (r *vmResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, err := configuredClient(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Cannot configure VM resource", err.Error())
		return
	}
	r.client = c
}

func (m *vmModel) size() tart.Size {
	return tart.Size{CPUCount: m.CPU.ValueInt64(), MemoryMiB: m.Memory.ValueInt64()}
}

func (m *vmModel) observe(vm tart.VM) {
	m.ID = m.Name
	m.OS = types.StringValue(vm.OS)
	m.CPU = types.Int64Value(vm.CPUCount)
	m.Memory = types.Int64Value(vm.MemoryMiB)
	m.Disk = types.Float64Value(vm.DiskGB)
}

func (*vmResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Name.IsUnknown() && !tart.ValidName(plan.Name.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Invalid Tart VM name", "Specify a valid local VM name.")
	}
	if !plan.CPU.IsUnknown() && plan.CPU.ValueInt64() < 1 {
		resp.Diagnostics.AddAttributeError(path.Root("cpu_count"), "Invalid CPU count", "Specify a positive CPU count.")
	}
	if !plan.Memory.IsUnknown() && plan.Memory.ValueInt64() < 1 {
		resp.Diagnostics.AddAttributeError(path.Root("memory_mib"), "Invalid memory size", "Specify a positive memory size in MiB.")
	}
	if !plan.Source.IsUnknown() && !plan.Source.IsNull() && (!tart.ValidName(plan.Source.ValueString()) || plan.Source.Equal(plan.Name)) {
		resp.Diagnostics.AddAttributeError(path.Root("source_image"), "Invalid Tart source", "Choose a separate local base VM.")
	}
	var prior vmModel
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if plan.Source.Equal(prior.Name) {
			resp.Diagnostics.AddAttributeError(path.Root("source_image"), "Invalid Tart source", "Choose a source separate from the VM being replaced.")
		}
	}
	if plan.Source.IsNull() && (req.State.Raw.IsNull() || !prior.Source.IsNull() || (!plan.Name.IsUnknown() && !prior.Name.Equal(plan.Name))) {
		resp.Diagnostics.AddAttributeError(path.Root("source_image"), "Source image required", "Specify a stopped local base to create a VM.")
	}
}

func (r *vmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	err := withWriteLock(ctx, r.client.Home(), func() error {
		if err := r.client.Clone(ctx, plan.Source.ValueString(), name); err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%w; import the existing VM or choose another name", err)
			}
			return err
		}
		vm, err := r.client.Resize(ctx, name, plan.size())
		if err != nil {
			return fmt.Errorf("clone %q was preserved; inspect it or import it before retrying: %w", name, err)
		}
		plan.observe(vm)
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot create Tart VM", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err := r.client.Get(ctx, state.Name.ValueString())
	if errors.Is(err, tart.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot refresh Tart VM", err.Error()+"; state was preserved. Inspect the VM before reconciling state.")
		return
	}
	state.observe(vm)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := withWriteLock(ctx, r.client.Home(), func() error {
		vm, err := r.client.Resize(ctx, plan.Name.ValueString(), plan.size())
		if err == nil {
			plan.observe(vm)
		}
		return err
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot update Tart VM", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := withWriteLock(ctx, r.client.Home(), func() error {
		return r.client.Delete(ctx, state.Name.ValueString())
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot delete Tart VM", err.Error())
	}
}

func (r *vmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
