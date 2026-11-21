package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type vmResource struct {
	client *client
}

type vmModel struct {
	Name       types.String  `tfsdk:"name"`
	Source     types.String  `tfsdk:"source_image"`
	Disposable types.Bool    `tfsdk:"disposable"`
	ID         types.String  `tfsdk:"id"`
	OS         types.String  `tfsdk:"os"`
	CPU        types.Int64   `tfsdk:"cpu_count"`
	Memory     types.Int64   `tfsdk:"memory_mib"`
	Disk       types.Float64 `tfsdk:"disk_size_gb"`
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
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{
		"name":         schema.StringAttribute{Required: true, Description: "Local VM name. Immutable after creation or import."},
		"source_image": schema.StringAttribute{Optional: true, Description: "Local stopped base image. Required for creation; omit for import."},
		"disposable": schema.BoolAttribute{Optional: true, Computed: true,
			Default: booldefault.StaticBool(false), Description: "Permit deletion of this provider-created VM only in a marked scratch store."},
		"id":           schema.StringAttribute{Computed: true},
		"os":           schema.StringAttribute{Computed: true},
		"cpu_count":    schema.Int64Attribute{Required: true},
		"memory_mib":   schema.Int64Attribute{Required: true},
		"disk_size_gb": schema.Float64Attribute{Computed: true, Description: "Virtual disk capacity as reported by Tart."},
	}}
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

func (m *vmModel) observe(vm observedVM) {
	m.ID = types.StringValue(vm.id)
	m.OS = types.StringValue(vm.OS)
	m.CPU = types.Int64Value(vm.CPU)
	m.Memory = types.Int64Value(vm.Memory)
	m.Disk = types.Float64Value(vm.Disk)
}

func (m vmModel) validate() error {
	if !vmName.MatchString(m.Name.ValueString()) {
		return fmt.Errorf("invalid local VM name %q", m.Name.ValueString())
	}
	if m.CPU.ValueInt64() < 1 || m.Memory.ValueInt64() < 1 {
		return errors.New("cpu_count and memory_mib must both be positive")
	}
	return nil
}

func sameSettings(plan, prior vmModel) bool {
	return plan.Name.Equal(prior.Name) && plan.Source.Equal(prior.Source) && plan.Disposable.Equal(prior.Disposable)
}

func (r *vmResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var prior vmModel
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if req.Plan.Raw.IsNull() {
		if !req.State.Raw.IsNull() {
			if err := r.checkDelete(ctx, prior); err != nil {
				resp.Diagnostics.AddError("Tart deletion blocked", err.Error())
			}
		}
		return
	}

	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !req.State.Raw.IsNull() && !sameSettings(plan, prior) {
		resp.Diagnostics.AddError("Tart replacement blocked",
			"VM name, source image, and disposal policy are immutable. Create a separate VM if you need a different image or name.")
		return
	}
	if req.State.Raw.IsNull() {
		if plan.Source.IsNull() {
			resp.Diagnostics.AddError("Source image required", "Set source_image to a stopped local Tart base VM to create a new VM.")
		}
		if plan.Disposable.ValueBool() {
			if err := r.client.requireScratch(); err != nil {
				resp.Diagnostics.AddError("Disposable VM creation blocked", err.Error())
			}
		}
		return
	}
	if plan.CPU.Equal(prior.CPU) && plan.Memory.Equal(prior.Memory) {
		return
	}
	vm, err := r.client.get(ctx, prior.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot plan Tart resize", err.Error())
		return
	}
	if vm.State != "stopped" {
		resp.Diagnostics.AddError("Tart resize blocked", "Stop the VM with Tart before changing CPU or memory.")
	}
}

func (r *vmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := plan.validate(); err != nil {
		resp.Diagnostics.AddError("Invalid Tart VM configuration", err.Error())
		return
	}
	name, source := plan.Name.ValueString(), plan.Source.ValueString()
	if !vmName.MatchString(source) || name == source {
		resp.Diagnostics.AddError("Invalid Tart source", "Specify a separate local VM name for source_image.")
		return
	}
	err := r.client.withWriteLock(ctx, func() error {
		if plan.Disposable.ValueBool() {
			if err := r.client.requireScratch(); err != nil {
				return err
			}
		}
		base, err := r.client.get(ctx, source)
		if err != nil {
			return fmt.Errorf("inspect source image: %w", err)
		}
		if base.State != "stopped" {
			return fmt.Errorf("source image %q must be stopped", source)
		}
		_, err = r.client.identity(name)
		if err == nil {
			return fmt.Errorf("destination %q already exists; import it or choose another name", name)
		}
		if !errors.Is(err, errVMMissing) {
			return err
		}
		if _, err := r.client.run(ctx, "clone", source, name); err != nil {
			return fmt.Errorf("clone failed; inspect destination %q before retrying: %w", name, err)
		}
		vm, err := r.client.get(ctx, name)
		if err == nil {
			vm, err = r.client.resize(ctx, name, vm, plan.CPU.ValueInt64(), plan.Memory.ValueInt64())
		}
		if err == nil && plan.Disposable.ValueBool() {
			err = r.client.recordOwnership(name, vm.id)
		}
		if err != nil {
			return fmt.Errorf("clone %q exists and was preserved; inspect it or import it before retrying: %w", name, err)
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
	vm, err := r.observed(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Cannot refresh Tart VM", err.Error()+"; state was preserved. Inspect the VM before reconciling state.")
		return
	}
	state.observe(vm)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmResource) observed(ctx context.Context, state vmModel) (observedVM, error) {
	vm, err := r.client.get(ctx, state.Name.ValueString())
	if err != nil {
		return observedVM{}, err
	}
	if vm.id != state.ID.ValueString() {
		return observedVM{}, errVMIdentity
	}
	return vm, nil
}

func (r *vmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := plan.validate(); err != nil {
		resp.Diagnostics.AddError("Invalid Tart VM configuration", err.Error())
		return
	}
	if !sameSettings(plan, prior) {
		resp.Diagnostics.AddError("Tart replacement blocked", "VM name, source image, and disposal policy are immutable.")
		return
	}
	err := r.client.withWriteLock(ctx, func() error {
		vm, err := r.observed(ctx, prior)
		if err != nil {
			return err
		}
		vm, err = r.client.resize(ctx, plan.Name.ValueString(), vm, plan.CPU.ValueInt64(), plan.Memory.ValueInt64())
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

func (r *vmResource) checkDelete(ctx context.Context, state vmModel) error {
	if !state.Disposable.ValueBool() {
		return errors.New("persistent and imported VMs cannot be deleted by this provider")
	}
	name, id := state.Name.ValueString(), state.ID.ValueString()
	if err := r.client.checkOwnership(name, id); err != nil {
		return fmt.Errorf("cannot verify disposable VM ownership: %w", err)
	}
	vm, err := r.observed(ctx, state)
	if err != nil {
		return err
	}
	if vm.State != "stopped" {
		return fmt.Errorf("disposable VM %q must be stopped before deletion", name)
	}
	return nil
}

func (r *vmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.withWriteLock(ctx, func() error {
		if err := r.checkDelete(ctx, state); err != nil {
			return err
		}
		name := state.Name.ValueString()
		if _, err := r.client.run(ctx, "delete", name); err != nil {
			return err
		}
		_, err := r.client.identity(name)
		if !errors.Is(err, errVMMissing) {
			return errors.New("Tart did not confirm the disposable VM was removed")
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Tart deletion blocked or failed", err.Error())
	}
}

func (r *vmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	vm, err := r.client.get(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Cannot import Tart VM", err.Error())
		return
	}
	state := vmModel{Name: types.StringValue(req.ID), Disposable: types.BoolValue(false)}
	state.observe(vm)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
