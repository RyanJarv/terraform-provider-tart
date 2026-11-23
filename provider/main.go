package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/ryanjarv/terraform-provider-tart/provider/internal/provider"
)

var address = "registry.terraform.io/ryanjarv/tart"
var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{Address: address})
	if err != nil {
		log.Fatal(err)
	}
}
