package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestUpgradeNameIdentity(t *testing.T) {
	ctx := context.Background()
	r := vmResource{}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	for version, permission := range []string{"disposable", "allow_deletion"} {
		raw := fmt.Sprintf(`{"name":"worker","source_image":"base","%s":true,"id":"old-id","os":"linux","cpu_count":2,"memory_mib":2048,"disk_size_gb":8}`, permission)
		req := resource.UpgradeStateRequest{RawState: &tfprotov6.RawState{JSON: []byte(raw)}}
		resp := resource.UpgradeStateResponse{State: tfsdk.State{Schema: schema.Schema}}
		r.UpgradeState(ctx)[int64(version)].StateUpgrader(ctx, req, &resp)
		var got vmModel
		resp.Diagnostics.Append(resp.State.Get(ctx, &got)...)
		if resp.Diagnostics.HasError() || got.ID.ValueString() != "worker" || got.Source.ValueString() != "base" {
			t.Fatalf("upgrade version %d: %+v, %v", version, got, resp.Diagnostics)
		}
	}
}
