// Package ci contains the infra required to setup a Github Actions pipeline with secure access to GCP
package ci

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/artifactregistry"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/iam"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// GithubGoogleRegistryStack represents the CI/CD infrastructure components
type GithubGoogleRegistryStack struct {
	RegistryURL                 pulumi.StringOutput
	WorkloadIdentityPool        *iam.WorkloadIdentityPool
	OidcProvider                *iam.WorkloadIdentityPoolProvider
	RepositoryPrincipalID       pulumi.StringOutput
	RepositoryIAMMembers        []*artifactregistry.RepositoryIamMember
	ProjectIAMMembers           []*projects.IAMMember
	GitHubActionsServiceAccount *serviceaccount.Account
}

// NewGithubGoogleRegistryStack creates CI/CD infrastructure for GitHub Actions
func NewGithubGoogleRegistryStack(ctx *pulumi.Context, config *Config) (*GithubGoogleRegistryStack, error) {
	// Set up Artifact Registry for Docker images
	repositoryName := fmt.Sprintf("%s-%s", config.ResourcePrefix, config.RepositoryName)
	repositoryName = capToMax(repositoryName, 63)

	registry, err := artifactregistry.NewRepository(ctx, repositoryName, &artifactregistry.RepositoryArgs{
		RepositoryId: pulumi.String(repositoryName),
		Location:     pulumi.String(config.RepositoryLocation),
		Project:      pulumi.String(config.GCPProject),
		Description:  pulumi.String("CI/CD Docker image registry"),
		Format:       pulumi.String("DOCKER"),
	})
	if err != nil {
		return nil, err
	}

	repoName := extractRepoName(config.AllowedRepoURL)

	// Create OIDC provider for GitHub Actions
	oidcProvider, workloadIdentityPool, err := newGithubActionsOIDCProvider(ctx, config, repoName)
	if err != nil {
		return nil, err
	}

	// Create service account and bind it to workload identity pool
	repoPrincipalID := pulumi.Sprintf(
		"principalSet://iam.googleapis.com/%s/attribute.repository/%s",
		workloadIdentityPool.Name,
		repoName,
	)

	// Repository-level roles (assigned to the specific repository)
	repoRoles := []string{
		"roles/artifactregistry.writer",
	}

	// Project-level roles (assigned at the project level)
	projectRoles := []string{
		// SBOM generation for container images
		"roles/containeranalysis.notes.editor",
		"roles/containeranalysis.occurrences.editor",
	}

	// Assign repository-level IAM roles
	repoIAMMembers := make([]*artifactregistry.RepositoryIamMember, 0, len(repoRoles))

	for _, role := range repoRoles {
		bindingName := fmt.Sprintf("%s-repo-iam-%s", config.ResourcePrefix, role)

		member, err := artifactregistry.NewRepositoryIamMember(ctx, bindingName, &artifactregistry.RepositoryIamMemberArgs{
			Repository: registry.Name,
			Location:   pulumi.String(config.RepositoryLocation),
			Project:    pulumi.String(config.GCPProject),
			Role:       pulumi.String(role),
			Member:     repoPrincipalID,
		})
		if err != nil {
			return nil, err
		}

		repoIAMMembers = append(repoIAMMembers, member)
	}

	// Assign project-level IAM roles
	projectIAMMembers := make([]*projects.IAMMember, 0, len(projectRoles))

	for _, role := range projectRoles {
		bindingName := fmt.Sprintf("%s-proj-iam-%s", config.ResourcePrefix, role)

		member, err := projects.NewIAMMember(ctx, bindingName, &projects.IAMMemberArgs{
			Project: pulumi.String(config.GCPProject),
			Role:    pulumi.String(role),
			Member:  repoPrincipalID,
		})
		if err != nil {
			return nil, err
		}

		projectIAMMembers = append(projectIAMMembers, member)
	}

	var githubActionsSA *serviceaccount.Account
	if config.CreateServiceAccount {
		githubActionsSA, err = newServiceAccountForDelegation(ctx, config)
		if err != nil {
			return nil, err
		}
	}

	// Create the registry URL
	registryURL := pulumi.Sprintf("%s-docker.pkg.dev/%s/%s", pulumi.String(config.RepositoryLocation), pulumi.String(config.GCPProject), registry.Name)

	return &GithubGoogleRegistryStack{
		RegistryURL:                 registryURL,
		RepositoryPrincipalID:       repoPrincipalID,
		RepositoryIAMMembers:        repoIAMMembers,
		ProjectIAMMembers:           projectIAMMembers,
		WorkloadIdentityPool:        workloadIdentityPool,
		OidcProvider:                oidcProvider,
		GitHubActionsServiceAccount: githubActionsSA,
	}, nil
}

func capToMax(identityProviderName string, maxLen int) string {
	if len(identityProviderName) > maxLen {
		identityProviderName = identityProviderName[:maxLen]
	}

	return identityProviderName
}

// newGithubActionsOIDCProvider creates a new OIDC provider for GitHub Actions
func newGithubActionsOIDCProvider(ctx *pulumi.Context, config *Config, repoName string) (*iam.WorkloadIdentityPoolProvider, *iam.WorkloadIdentityPool, error) {
	// Create OIDC workload identity pool for GitHub Actions
	identityPoolName := fmt.Sprintf("%s-github-actions-pool", config.ResourcePrefix)
	identityPoolName = capToMax(identityPoolName, 32)

	identityPool, err := iam.NewWorkloadIdentityPool(ctx, identityPoolName, &iam.WorkloadIdentityPoolArgs{
		WorkloadIdentityPoolId: pulumi.String(identityPoolName),
		Project:                pulumi.String(config.GCPProject),
		DisplayName:            pulumi.String("GitHub Actions Workload Pool"),
		Description:            pulumi.String("Workload identity pool for GitHub Actions"),
		Disabled:               pulumi.Bool(false),
	})
	if err != nil {
		return nil, nil, err
	}

	// Create OIDC provider for GitHub Actions
	identityProviderName := fmt.Sprintf("%s-%s", config.ResourcePrefix, config.IdentityPoolProviderName)
	identityProviderName = capToMax(identityProviderName, 32)

	oidcProvider, err := iam.NewWorkloadIdentityPoolProvider(ctx, identityProviderName, &iam.WorkloadIdentityPoolProviderArgs{
		WorkloadIdentityPoolId:         identityPool.WorkloadIdentityPoolId,
		WorkloadIdentityPoolProviderId: pulumi.String(identityProviderName),
		Project:                        pulumi.String(config.GCPProject),
		DisplayName:                    pulumi.String("GitHub Actions OIDC Provider"),
		Description:                    pulumi.String("OIDC provider for GitHub Actions"),
		Disabled:                       pulumi.Bool(false),
		AttributeMapping: pulumi.StringMap{
			"google.subject":                pulumi.String("assertion.sub"),
			"attribute.repository":          pulumi.String("assertion.repository"),
			"attribute.repository_owner":    pulumi.String("assertion.repository_owner"),
			"attribute.repository_owner_id": pulumi.String("assertion.repository_owner_id"),
			"attribute.repository_id":       pulumi.String("assertion.repository_id"),
			"attribute.actor":               pulumi.String("assertion.actor"),
			"attribute.ref":                 pulumi.String("assertion.ref"),
			"attribute.sha":                 pulumi.String("assertion.sha"),
			"attribute.workflow":            pulumi.String("assertion.workflow"),
			"attribute.head_ref":            pulumi.String("assertion.head_ref"),
			"attribute.base_ref":            pulumi.String("assertion.base_ref"),
			"attribute.aud":                 pulumi.String("assertion.aud"),
		},
		Oidc: &iam.WorkloadIdentityPoolProviderOidcArgs{
			IssuerUri: pulumi.String("https://token.actions.githubusercontent.com"),
		},
		AttributeCondition: pulumi.String(buildAttributeCondition(repoName, config)),
	})
	if err != nil {
		return nil, nil, err
	}

	return oidcProvider, identityPool, nil
}

// newServiceAccountForDelegation creates a service account and binds it to the workload identity pool
func newServiceAccountForDelegation(ctx *pulumi.Context, config *Config) (*serviceaccount.Account, error) {
	// Create a service account for GitHub Actions
	serviceAccountName := fmt.Sprintf("%s-github-actions-sa", config.ResourcePrefix)
	serviceAccountName = capToMax(serviceAccountName, 30)

	githubActionsSA, err := serviceaccount.NewAccount(ctx, serviceAccountName, &serviceaccount.AccountArgs{
		AccountId:   pulumi.String(serviceAccountName),
		Project:     pulumi.String(config.GCPProject),
		DisplayName: pulumi.String("GitHub Actions Service Account"),
		Description: pulumi.String("Service account for GitHub Actions CI/CD"),
	})
	if err != nil {
		return nil, err
	}

	// Bind the service account to the workload identity pool
	// This allows the service account to be impersonated by the workload identity pool
	_, err = serviceaccount.NewIAMMember(ctx, fmt.Sprintf("%s-workload-identity-user", config.ResourcePrefix), &serviceaccount.IAMMemberArgs{
		ServiceAccountId: githubActionsSA.Name,
		Role:             pulumi.String("roles/iam.workloadIdentityUser"),
		Member:           pulumi.Sprintf("serviceAccount:%s", githubActionsSA.Email),
	})
	if err != nil {
		return nil, err
	}

	return githubActionsSA, nil
}

// extractRepoName extracts the repository name from a GitHub URL
func extractRepoName(repoURL string) string {
	if len(repoURL) > 19 && repoURL[:19] == "https://github.com/" {
		return repoURL[19:]
	}

	return repoURL
}

// buildAttributeCondition creates a secure attribute condition for the OIDC provider
func buildAttributeCondition(repoName string, config *Config) string {
	// Start with repository constraint
	condition := fmt.Sprintf(`attribute.repository == "%s"`, repoName)

	// Add repository owner constraint if provided
	if config.RepositoryOwner != "" {
		condition += fmt.Sprintf(` && attribute.repository_owner == "%s"`, config.RepositoryOwner)
	}

	// Add repository owner ID constraint if provided (recommended for security)
	if config.RepositoryOwnerID != "" {
		condition += fmt.Sprintf(` && attribute.repository_owner_id == "%s"`, config.RepositoryOwnerID)
	}

	// Add repository ID constraint if provided (recommended for security)
	if config.RepositoryID != "" {
		condition += fmt.Sprintf(` && attribute.repository_id == "%s"`, config.RepositoryID)
	}

	return condition
}
