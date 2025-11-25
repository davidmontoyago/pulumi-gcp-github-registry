// Package ci contains the infra required to setup a Github Actions pipeline with secure access to GCP
package ci

import (
	"fmt"

	namer "github.com/davidmontoyago/commodity-namer"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/artifactregistry"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/iam"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/organizations"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/storage"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// GithubGoogleRegistry represents the CI/CD infrastructure components
type GithubGoogleRegistry struct {
	pulumi.ResourceState
	namer.Namer

	RegistryURL                 pulumi.StringOutput
	WorkloadIdentityPool        *iam.WorkloadIdentityPool
	OidcProvider                *iam.WorkloadIdentityPoolProvider
	RepositoryPrincipalID       pulumi.StringOutput
	RepositoryIAMMembers        []*artifactregistry.RepositoryIamMember
	ProjectIAMMembers           []*projects.IAMMember
	GitHubActionsServiceAccount *serviceaccount.Account
	SBOMBucket                  *storage.Bucket
	SBOMBucketIAMMember         *storage.BucketIAMMember

	// This is the resulting workload identity provider that must be passed in the Github auth action call
	WorkloadIdentityPoolProviderID pulumi.StringOutput

	repositoryName string
	config         *Config
}

// NewGithubGoogleRegistry creates CI/CD infrastructure for GitHub Actions
func NewGithubGoogleRegistry(ctx *pulumi.Context, config *Config, opts ...pulumi.ResourceOption) (*GithubGoogleRegistry, error) {
	// Set up Artifact Registry for Docker images
	registry := &GithubGoogleRegistry{
		Namer:          namer.New(config.ResourcePrefix),
		repositoryName: config.RepositoryName,
		config:         config,
	}

	componentName := fmt.Sprintf("%s-%s", config.ResourcePrefix, config.RepositoryName)

	err := ctx.RegisterComponentResource("pulumi-gcp-github-registry:ci:GithubGoogleRegistry", componentName, registry, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to register component resource: %w", err)
	}

	err = registry.deploy(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy component resources: %w", err)
	}

	return registry, nil
}

// NewGithubGoogleRegistry creates CI/CD infrastructure for GitHub Actions
func (r *GithubGoogleRegistry) deploy(ctx *pulumi.Context) error {
	registryAPI, err := r.enableRegistryAPI(ctx)
	if err != nil {
		return fmt.Errorf("failed to enable Artifact Registry API: %w", err)
	}

	repoResourceName := r.NewResourceName(r.repositoryName, "repo", 63)
	// The input controls the ID, we just make sure it's valid
	repositoryID := r.NewResourceName(r.repositoryName, "", 63)

	registry, err := artifactregistry.NewRepository(ctx, repoResourceName, &artifactregistry.RepositoryArgs{
		RepositoryId: pulumi.String(repositoryID),
		Location:     pulumi.String(r.config.RepositoryLocation),
		Project:      pulumi.String(r.config.GCPProject),
		Description:  pulumi.String("CI/CD Docker image registry"),
		Format:       pulumi.String("DOCKER"),
		Labels: pulumi.StringMap{
			"managed-by": pulumi.String("pulumi"),
			"purpose":    pulumi.String("docker-images"),
		},
	},
		pulumi.Parent(r),
		pulumi.Protect(r.config.ProtectResources),
		pulumi.DependsOn([]pulumi.Resource{registryAPI}),
	)
	if err != nil {
		return fmt.Errorf("failed to create artifact registry repository: %w", err)
	}

	repoName := extractRepoName(r.config.AllowedRepoURL)

	// Create OIDC provider for GitHub Actions
	oidcProvider, workloadIdentityPool, err := r.newGithubActionsOIDCProvider(ctx, r.config, repoName)
	if err != nil {
		return fmt.Errorf("failed to create OIDC provider for GitHub Actions: %w", err)
	}

	// Create service account and bind it to workload identity pool
	repoPrincipalID := pulumi.Sprintf(
		"principalSet://iam.googleapis.com/%s/attribute.repository/%s",
		workloadIdentityPool.Name,
		repoName,
	)

	// Grant IAM permissions to the pipeline
	repoIAMMembers, projectIAMMembers, err := r.grantPipelineIAM(ctx, r.config, registry, repoPrincipalID)
	if err != nil {
		return fmt.Errorf("failed to grant IAM permissions to the pipeline: %w", err)
	}

	// Create SBOM bucket for storing Software Bill of Materials
	sbomBucket, sbomBucketIAMMember, err := r.createSBOMsBucket(ctx, r.config, repoPrincipalID)
	if err != nil {
		return fmt.Errorf("failed to create SBOM bucket: %w", err)
	}

	var githubActionsSA *serviceaccount.Account
	if r.config.CreateServiceAccount {
		githubActionsSA, err = r.newServiceAccountForDelegation(ctx, r.config)
		if err != nil {
			return fmt.Errorf("failed to create service account for delegation: %w", err)
		}
	}

	// Create the registry URL
	registryURL := pulumi.Sprintf("%s-docker.pkg.dev/%s/%s", pulumi.String(r.config.RepositoryLocation), pulumi.String(r.config.GCPProject), registry.RepositoryId)

	// Create the workload identity provider ID to set in the Github auth action
	// Numeric project ID is required
	project, err := organizations.GetProject(ctx, "get-project", pulumi.ID(r.config.GCPProject), nil)
	if err != nil {
		return fmt.Errorf("failed to get project numeric ID: %w", err)
	}

	workloadIdentityPoolProviderID := pulumi.Sprintf(
		"projects/%s/locations/global/workloadIdentityPools/%s/providers/%s",
		project.Number,
		workloadIdentityPool.WorkloadIdentityPoolId,
		oidcProvider.WorkloadIdentityPoolProviderId,
	)

	// Set the outputs
	r.RegistryURL = registryURL
	r.WorkloadIdentityPoolProviderID = workloadIdentityPoolProviderID
	r.RepositoryPrincipalID = repoPrincipalID
	r.RepositoryIAMMembers = repoIAMMembers
	r.ProjectIAMMembers = projectIAMMembers
	r.WorkloadIdentityPool = workloadIdentityPool
	r.OidcProvider = oidcProvider
	r.GitHubActionsServiceAccount = githubActionsSA
	r.SBOMBucket = sbomBucket
	r.SBOMBucketIAMMember = sbomBucketIAMMember

	return nil
}

// grantPipelineIAM grants IAM permissions to the GitHub Actions pipeline
func (r *GithubGoogleRegistry) grantPipelineIAM(ctx *pulumi.Context, config *Config, registry *artifactregistry.Repository, repoPrincipalID pulumi.StringOutput) ([]*artifactregistry.RepositoryIamMember, []*projects.IAMMember, error) {
	// Repository-level roles (assigned to the specific repository)
	repoRoles := []string{
		"roles/artifactregistry.writer",
	}

	// Project-level roles (assigned at the project level)
	projectRoles := []string{
		// SBOM generation for container images
		// See: https://cloud.google.com/artifact-analysis/docs/generate-store-sboms
		"roles/containeranalysis.notes.editor",
		"roles/containeranalysis.occurrences.editor",
		"roles/storage.bucketViewer",
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
		}, pulumi.Parent(r))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create repository IAM member: %w", err)
		}

		repoIAMMembers = append(repoIAMMembers, member)
	}

	// Assign project-level IAM roles
	projectIAMMembers := make([]*projects.IAMMember, 0, len(projectRoles))

	for _, role := range projectRoles {
		bindingName := fmt.Sprintf("%s-project-iam-%s", config.ResourcePrefix, role)

		member, err := projects.NewIAMMember(ctx, bindingName, &projects.IAMMemberArgs{
			Project: pulumi.String(config.GCPProject),
			Role:    pulumi.String(role),
			Member:  repoPrincipalID,
		}, pulumi.Parent(r))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create project IAM member: %w", err)
		}

		projectIAMMembers = append(projectIAMMembers, member)
	}

	return repoIAMMembers, projectIAMMembers, nil
}

// createSBOMsBucket creates a GCS bucket for storing SBOMs with proper IAM permissions
func (r *GithubGoogleRegistry) createSBOMsBucket(ctx *pulumi.Context, config *Config, repoPrincipalID pulumi.StringOutput) (*storage.Bucket, *storage.BucketIAMMember, error) {
	// Default bucket name for SBOMs: artifacts-{project-id}-sbom
	bucketName := fmt.Sprintf("artifacts-%s-sbom", config.GCPProject)

	// Create the bucket with best practices for security and compliance
	bucket, err := storage.NewBucket(ctx, bucketName, &storage.BucketArgs{
		Name:         pulumi.String(bucketName),
		Location:     pulumi.String(config.GCPRegion),
		Project:      pulumi.String(config.GCPProject),
		ForceDestroy: pulumi.Bool(false), // Prevent accidental deletion
		Versioning: &storage.BucketVersioningArgs{
			Enabled: pulumi.Bool(true), // Enable versioning for audit trail
		},
		LifecycleRules: storage.BucketLifecycleRuleArray{
			&storage.BucketLifecycleRuleArgs{
				Action: &storage.BucketLifecycleRuleActionArgs{
					Type: pulumi.String("Delete"),
				},
				Condition: &storage.BucketLifecycleRuleConditionArgs{
					Age: pulumi.Int(365), // Keep SBOMs for 1 year
				},
			},
		},
		Labels: pulumi.StringMap{
			"purpose":    pulumi.String("sbom-storage"),
			"managed-by": pulumi.String("pulumi"),
		},
		// Prevent public access to the bucket for security
		PublicAccessPrevention: pulumi.String("enforced"),
		// Enable Uniform Bucket Level Access (UBLA) for enhanced security
		// This is required for SBOMs and prevents ACL-based access control
		UniformBucketLevelAccess: pulumi.Bool(true),
	}, pulumi.Parent(r))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create SBOM bucket: %w", err)
	}

	// Grant object admin role to the repository principal for SBOM uploads
	bucketIAMMember, err := storage.NewBucketIAMMember(ctx, fmt.Sprintf("%s-sbom-bucket-iam", config.ResourcePrefix), &storage.BucketIAMMemberArgs{
		Bucket: bucket.Name,
		Role:   pulumi.String("roles/storage.objectAdmin"),
		Member: repoPrincipalID,
	}, pulumi.Parent(r))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create SBOM bucket IAM member: %w", err)
	}

	return bucket, bucketIAMMember, nil
}

func capToMax(identityProviderName string, maxLen int) string {
	if len(identityProviderName) > maxLen {
		identityProviderName = identityProviderName[:maxLen]
	}

	return identityProviderName
}

// newGithubActionsOIDCProvider creates a new OIDC provider for GitHub Actions
func (r *GithubGoogleRegistry) newGithubActionsOIDCProvider(ctx *pulumi.Context, config *Config, repoName string) (*iam.WorkloadIdentityPoolProvider, *iam.WorkloadIdentityPool, error) {
	// Create OIDC workload identity pool for GitHub Actions
	identityPoolName := fmt.Sprintf("%s-github-actions-pool", config.ResourcePrefix)
	identityPoolName = capToMax(identityPoolName, 32)

	identityPool, err := iam.NewWorkloadIdentityPool(ctx, identityPoolName, &iam.WorkloadIdentityPoolArgs{
		WorkloadIdentityPoolId: pulumi.String(identityPoolName),
		Project:                pulumi.String(config.GCPProject),
		DisplayName:            pulumi.String("GitHub Actions Workload Pool"),
		Description:            pulumi.String("Workload identity pool for GitHub Actions"),
		Disabled:               pulumi.Bool(false),
	}, pulumi.Parent(r))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OIDC provider for GitHub Actions: %w", err)
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
	}, pulumi.Parent(r))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OIDC provider for GitHub Actions: %w", err)
	}

	return oidcProvider, identityPool, nil
}

// newServiceAccountForDelegation creates a service account and binds it to the workload identity pool
func (r *GithubGoogleRegistry) newServiceAccountForDelegation(ctx *pulumi.Context, config *Config) (*serviceaccount.Account, error) {
	// Create a service account for GitHub Actions
	serviceAccountName := fmt.Sprintf("%s-github-actions-sa", config.ResourcePrefix)
	serviceAccountName = capToMax(serviceAccountName, 30)

	githubActionsSA, err := serviceaccount.NewAccount(ctx, serviceAccountName, &serviceaccount.AccountArgs{
		AccountId:   pulumi.String(serviceAccountName),
		Project:     pulumi.String(config.GCPProject),
		DisplayName: pulumi.String("GitHub Actions Service Account"),
		Description: pulumi.String("Service account for GitHub Actions CI/CD"),
	}, pulumi.Parent(r))
	if err != nil {
		return nil, fmt.Errorf("failed to create service account for delegation: %w", err)
	}

	// Bind the service account to the workload identity pool
	// This allows the service account to be impersonated by the workload identity pool
	_, err = serviceaccount.NewIAMMember(ctx, fmt.Sprintf("%s-workload-identity-user", config.ResourcePrefix), &serviceaccount.IAMMemberArgs{
		ServiceAccountId: githubActionsSA.Name,
		Role:             pulumi.String("roles/iam.workloadIdentityUser"),
		Member:           pulumi.Sprintf("serviceAccount:%s", githubActionsSA.Email),
	}, pulumi.Parent(r))
	if err != nil {
		return nil, fmt.Errorf("failed to create service account IAM member: %w", err)
	}

	return githubActionsSA, nil
}

func (r *GithubGoogleRegistry) enableRegistryAPI(ctx *pulumi.Context) (*projects.Service, error) {
	service, err := projects.NewService(ctx, r.NewResourceName("artifactregistry", "api", 63), &projects.ServiceArgs{
		Project:                  pulumi.String(r.config.GCPProject),
		Service:                  pulumi.String("artifactregistry.googleapis.com"),
		DisableOnDestroy:         pulumi.Bool(false),
		DisableDependentServices: pulumi.Bool(false),
	},
		pulumi.Parent(r),
		pulumi.RetainOnDelete(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to enable Artifact Registry API: %w", err)
	}

	return service, nil
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
