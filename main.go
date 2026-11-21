package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var address = "registry.terraform.io/ryanjarv/tart"
var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), func() provider.Provider {
		return &tartProvider{version: version}
	}, providerserver.ServeOpts{Address: address})
	if err != nil {
		log.Fatal(err)
	}
}
