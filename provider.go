package main

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type tartProvider struct {
	version string
}

type providerConfig struct {
	TartPath types.String `tfsdk:"tart_path"`
	TartHome types.String `tfsdk:"tart_home"`
}

func (p *tartProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tart"
	resp.Version = p.version
}

func (*tartProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{
		"tart_path": schema.StringAttribute{Optional: true, Description: "Absolute Tart executable path. Defaults to /opt/homebrew/bin/tart."},
		"tart_home": schema.StringAttribute{Required: true, Description: "Absolute path to the existing Tart store managed by this provider instance."},
	}}
}

func (*tartProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerConfig
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.TartHome.IsUnknown() || config.TartPath.IsUnknown() {
		resp.Diagnostics.AddError("Tart configuration must be known", "Set tart_home and any tart_path before planning.")
		return
	}
	path := config.TartPath.ValueString()
	if config.TartPath.IsNull() {
		path = "/opt/homebrew/bin/tart"
	}
	c, err := newClient(path, config.TartHome.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot configure Tart", err.Error())
		return
	}
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (*tartProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{func() datasource.DataSource { return &vmDataSource{} }}
}

func (*tartProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource { return &vmResource{} }}
}

func configuredClient(value any) (*client, error) {
	c, ok := value.(*client)
	if !ok || c == nil {
		return nil, fmt.Errorf("expected a configured Tart client, got %T", value)
	}
	return c, nil
}
