package main

import (
	"log"

	"github.com/davidmontoyago/pulumi-gcp-github-registry/deploy/ci"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Load configuration from environment variables
		config, err := ci.LoadConfig()
		if err != nil {
			return err
		}

		// Log the stack and project for verification
		log.Printf("Deploying to stack: %s", ctx.Stack())
		log.Printf("GCP Project: %s", config.GCPProject)
		log.Printf("GCP Region: %s", config.GCPRegion)

		// Create CI/CD infrastructure
		ciInfra, err := ci.NewCiInfrastructure(ctx, config)
		if err != nil {
			return err
		}

		// Export the outputs for use in CI/CD
		ctx.Export("registryURL", ciInfra.RegistryUrl)
		ctx.Export("serviceAccountEmail", ciInfra.ServiceAccountEmail)
		ctx.Export("workloadIdentityPoolID", pulumi.ToSecret(ciInfra.WorkloadIdentityPool.ID()))
		ctx.Export("workloadIdentityProviderID", pulumi.ToSecret(ciInfra.OidcProvider.ID()))
		ctx.Export("workloadIdentityProviderURN", pulumi.ToSecret(ciInfra.OidcProvider.URN()))
		ctx.Export("workloadIdentityProviderAllowedRepo", ciInfra.OidcProvider.AttributeCondition)

		log.Println("CI/CD infrastructure deployment loaded and ready!")
		return nil
	})
}
