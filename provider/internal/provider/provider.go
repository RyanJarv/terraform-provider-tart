package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ryanjarv/terraform-provider-tart/tart"
)

type tartProvider struct {
	version string
}

// New returns the factory used to serve the Tart provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &tartProvider{version: version}
	}
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
		"tart_path": schema.StringAttribute{Optional: true, Description: "Absolute Tart executable path. Defaults to tart on PATH."},
		"tart_home": schema.StringAttribute{Optional: true, Description: "Absolute path to the existing Tart store. Defaults to TART_HOME or ~/.tart."},
	}}
}

func (*tartProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerConfig
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.TartHome.IsUnknown() || config.TartPath.IsUnknown() {
		resp.Diagnostics.AddError("Tart configuration must be known", "Any configured tart_home and tart_path must be known before planning.")
		return
	}
	if (!config.TartPath.IsNull() && config.TartPath.ValueString() == "") ||
		(!config.TartHome.IsNull() && config.TartHome.ValueString() == "") {
		resp.Diagnostics.AddError("Cannot configure Tart", "Configured tart_path and tart_home must be absolute paths.")
		return
	}
	cli, err := tart.NewClient(tart.Config{Executable: config.TartPath.ValueString(), Home: config.TartHome.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Cannot configure Tart", err.Error())
		return
	}
	resp.DataSourceData = cli
	resp.ResourceData = cli
}

func (*tartProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{func() datasource.DataSource { return &vmDataSource{} }}
}

func (*tartProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource { return &vmResource{} }}
}

func configuredClient(value any) (*tart.Client, error) {
	c, ok := value.(*tart.Client)
	if !ok || c == nil {
		return nil, fmt.Errorf("expected a configured Tart client, got %T", value)
	}
	return c, nil
}
