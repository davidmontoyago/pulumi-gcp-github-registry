package ci

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/artifactregistry"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/iam"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// CiInfrastructure represents the CI/CD infrastructure components
type CiInfrastructure struct {
	Registry                    *artifactregistry.Repository
	GitHubActionsServiceAccount *serviceaccount.Account
	WorkloadIdentityPool        *iam.WorkloadIdentityPool
	OidcProvider                *iam.WorkloadIdentityPoolProvider
	RegistryUrl                 pulumi.StringOutput
	ServiceAccountEmail         pulumi.StringOutput
	WorkloadIdentityPoolId      pulumi.StringOutput
	WorkloadIdentityProviderId  pulumi.StringOutput
	ServiceAccountOidcMember    *serviceaccount.IAMMember
}

// NewCiInfrastructure creates CI/CD infrastructure for GitHub Actions
func NewCiInfrastructure(ctx *pulumi.Context, config *Config) (*CiInfrastructure, error) {
	// Set up Artifact Registry for Docker images
	registry, err := artifactregistry.NewRepository(ctx, fmt.Sprintf("%s-%s", config.ResourcePrefix, config.RepositoryName), &artifactregistry.RepositoryArgs{
		RepositoryId: pulumi.String(config.RepositoryName),
		Location:     pulumi.String("us"),
		Project:      pulumi.String(config.GCPProject),
		Description:  pulumi.String("CI/CD Docker image registry"),
		Format:       pulumi.String("DOCKER"),
	})
	if err != nil {
		return nil, err
	}

	// Create a service account for GitHub Actions
	githubActionsSA, err := serviceaccount.NewAccount(ctx, fmt.Sprintf("%s-github-actions-sa", config.ResourcePrefix), &serviceaccount.AccountArgs{
		AccountId:   pulumi.Sprintf("%s-github-actions", pulumi.String(config.ResourcePrefix)),
		Project:     pulumi.String(config.GCPProject),
		DisplayName: pulumi.String("GitHub Actions Service Account"),
		Description: pulumi.String("Service account for GitHub Actions CI/CD"),
	})
	if err != nil {
		return nil, err
	}

	// Create OIDC workload identity pool for GitHub Actions
	workloadIdentityPool, err := iam.NewWorkloadIdentityPool(ctx, fmt.Sprintf("%s-github-actions-pool", config.ResourcePrefix), &iam.WorkloadIdentityPoolArgs{
		WorkloadIdentityPoolId: pulumi.Sprintf("%s-github-actions-pool", pulumi.String(config.ResourcePrefix)),
		Project:                pulumi.String(config.GCPProject),
		DisplayName:            pulumi.String("GitHub Actions Workload Pool"),
		Description:            pulumi.String("Workload identity pool for GitHub Actions"),
		Disabled:               pulumi.Bool(false),
	})
	if err != nil {
		return nil, err
	}

	// Create OIDC provider for GitHub Actions
	// Calculate the full resource name and cap it to 32 characters
	resourceName := fmt.Sprintf("%s-%s", config.ResourcePrefix, config.IdentityPoolProviderName)
	if len(resourceName) > 32 {
		// Truncate from the right, preserving the prefix
		prefixLength := len(config.ResourcePrefix) + 1 // +1 for the hyphen
		remainingLength := 32 - prefixLength
		if remainingLength > 0 {
			resourceName = fmt.Sprintf("%s-%s", config.ResourcePrefix, config.IdentityPoolProviderName[:remainingLength])
		} else {
			// If prefix is too long, just use the prefix
			resourceName = config.ResourcePrefix
		}
	}

	repoName := config.AllowedRepoURL
	if len(config.AllowedRepoURL) > 19 && config.AllowedRepoURL[:19] == "https://github.com/" {
		repoName = config.AllowedRepoURL[19:]
	}

	oidcProvider, err := iam.NewWorkloadIdentityPoolProvider(ctx, resourceName, &iam.WorkloadIdentityPoolProviderArgs{
		WorkloadIdentityPoolId:         workloadIdentityPool.WorkloadIdentityPoolId,
		WorkloadIdentityPoolProviderId: pulumi.String(config.IdentityPoolProviderName),
		Project:                        pulumi.String(config.GCPProject),
		DisplayName:                    pulumi.String("GitHub Actions OIDC Provider"),
		Description:                    pulumi.String("OIDC provider for GitHub Actions"),
		Disabled:                       pulumi.Bool(false),
		AttributeMapping: pulumi.StringMap{
			"google.subject":       pulumi.String("assertion.sub"),
			"attribute.repository": pulumi.String("assertion.repository"),
			"attribute.actor":      pulumi.String("assertion.actor"),
			"attribute.aud":        pulumi.String("assertion.aud"),
		},
		Oidc: &iam.WorkloadIdentityPoolProviderOidcArgs{
			IssuerUri: pulumi.String("https://token.actions.githubusercontent.com"),
		},
		AttributeCondition: pulumi.Sprintf(`attribute.repository == "%s"`, repoName),
	})
	if err != nil {
		return nil, err
	}

	// Bind the service account to the workload identity pool
	// This allows the service account to be impersonated by the workload identity pool
	oidcMember, err := serviceaccount.NewIAMMember(ctx, fmt.Sprintf("%s-workload-identity-user", config.ResourcePrefix), &serviceaccount.IAMMemberArgs{
		ServiceAccountId: githubActionsSA.Name,
		Role:             pulumi.String("roles/iam.workloadIdentityUser"),
		Member:           pulumi.Sprintf("principalSet://iam.googleapis.com/%s/attribute.repository/%s", workloadIdentityPool.Name, repoName),
	})
	if err != nil {
		return nil, err
	}

	// Grant the service account access to Artifact Registry
	_, err = artifactregistry.NewRepositoryIamMember(ctx, fmt.Sprintf("%s-registry-writer", config.ResourcePrefix), &artifactregistry.RepositoryIamMemberArgs{
		Repository: registry.Name,
		Location:   pulumi.String("us"),
		Project:    pulumi.String(config.GCPProject),
		Role:       pulumi.String("roles/artifactregistry.writer"),
		Member:     pulumi.Sprintf("serviceAccount:%s", githubActionsSA.Email),
	})
	if err != nil {
		return nil, err
	}

	// Create the registry URL
	registryUrl := pulumi.Sprintf("us-docker.pkg.dev/%s/%s", pulumi.String(config.GCPProject), pulumi.String(config.RepositoryName))

	return &CiInfrastructure{
		Registry:                    registry,
		GitHubActionsServiceAccount: githubActionsSA,
		ServiceAccountOidcMember:    oidcMember,
		WorkloadIdentityPool:        workloadIdentityPool,
		OidcProvider:                oidcProvider,
		RegistryUrl:                 registryUrl,
	}, nil
}
