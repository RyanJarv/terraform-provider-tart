package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ryanjarv/terraform-provider-tart/tart"
)

type vmDataSource struct {
	client *tart.Client
}

type vmDataModel struct {
	Name   types.String  `tfsdk:"name"`
	ID     types.String  `tfsdk:"id"`
	OS     types.String  `tfsdk:"os"`
	CPU    types.Int64   `tfsdk:"cpu_count"`
	Memory types.Int64   `tfsdk:"memory_mib"`
	Disk   types.Float64 `tfsdk:"disk_size_gb"`
	Status types.String  `tfsdk:"status"`
}

func (*vmDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm"
}

func (*vmDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{
		"name":         schema.StringAttribute{Required: true},
		"id":           schema.StringAttribute{Computed: true, Description: "VM name in the configured Tart store."},
		"os":           schema.StringAttribute{Computed: true},
		"cpu_count":    schema.Int64Attribute{Computed: true},
		"memory_mib":   schema.Int64Attribute{Computed: true},
		"disk_size_gb": schema.Float64Attribute{Computed: true, Description: "Virtual disk capacity as reported by Tart."},
		"status":       schema.StringAttribute{Computed: true, Description: "Runtime status observed during refresh."},
	}}
}

func (d *vmDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, err := configuredClient(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Cannot configure VM lookup", err.Error())
		return
	}
	d.client = c
}

func (d *vmDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data vmDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err := d.client.Get(ctx, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read Tart VM", err.Error())
		return
	}
	data.ID = data.Name
	data.OS = types.StringValue(vm.OS)
	data.CPU = types.Int64Value(vm.CPUCount)
	data.Memory = types.Int64Value(vm.MemoryMiB)
	data.Disk = types.Float64Value(vm.DiskGB)
	data.Status = types.StringValue(vm.State)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
