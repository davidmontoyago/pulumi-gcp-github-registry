package ci_test

import (
	"testing"

	"github.com/davidmontoyago/pulumi-gcp-github-registry/deploy/ci"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type infraMocks struct{}

func (m *infraMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	// Mock resource outputs for each resource type:
	//
	// gcp:artifactregistry/repository:Repository
	//   - name: string (resource name)
	//   - repositoryId: string (repository identifier)
	//   - location: string (repository location, e.g., "us")
	//   - project: string (GCP project ID)
	//   - format: string (repository format, e.g., "DOCKER")
	//
	// gcp:serviceaccount/account:Account
	//   - name: string (resource name)
	//   - accountId: string (service account ID)
	//   - project: string (GCP project ID)
	//   - displayName: string (human-readable name)
	//   - email: string (service account email, computed)
	//
	// gcp:iam/workloadIdentityPool:WorkloadIdentityPool
	//   - name: string (resource name)
	//   - workloadIdentityPoolId: string (pool identifier)
	//   - project: string (GCP project ID)
	//   - displayName: string (human-readable name)
	//   - description: string (pool description)
	//   - disabled: bool (whether pool is disabled)
	//
	// gcp:iam/workloadIdentityPoolProvider:WorkloadIdentityPoolProvider
	//   - name: string (resource name)
	//   - workloadIdentityPoolProviderId: string (provider identifier)
	//   - project: string (GCP project ID)
	//   - displayName: string (human-readable name)
	//   - description: string (provider description)
	//   - disabled: bool (whether provider is disabled)
	//   - attributeMapping: map[string]string (OIDC attribute mappings)
	//   - attributeCondition: string (CEL condition for attribute filtering)
	//   - oidc: map[string]interface{} (OIDC configuration with issuerUri)
	//
	// gcp:serviceaccount/iAMMember:IAMMember
	//   - role: string (IAM role, e.g., "roles/iam.workloadIdentityUser")
	//   - member: string (principal to bind, e.g., "principalSet://...")
	//   - serviceAccountId: string (service account identifier)
	//
	// gcp:artifactregistry/repositoryIamMember:RepositoryIamMember
	//   - role: string (IAM role, e.g., "roles/artifactregistry.writer")
	//   - member: string (principal to bind, e.g., "serviceAccount:...")
	//   - repository: string (repository name reference)
	//
	// gcp:projects/iAMMember:IAMMember
	//   - role: string (IAM role, e.g., "roles/containeranalysis.notes.editor")
	//   - member: string (principal to bind, e.g., "principalSet://...")
	//   - project: string (project identifier)
	//
	// gcp:storage/bucket:Bucket
	//   - name: string (bucket name)
	//   - location: string (bucket location)
	//   - project: string (GCP project ID)
	//   - versioning: map[string]interface{} (versioning configuration)
	//   - lifecycleRules: array (lifecycle rules configuration)
	//   - labels: map[string]string (bucket labels)
	//
	// gcp:storage/bucketIAMMember:BucketIAMMember
	//   - bucket: string (bucket name reference)
	//   - role: string (IAM role, e.g., "roles/storage.objectAdmin")
	//   - member: string (principal to bind, e.g., "principalSet://...")
	outputs := map[string]interface{}{}
	for k, v := range args.Inputs {
		outputs[string(k)] = v
	}

	switch args.TypeToken {
	case "gcp:artifactregistry/repository:Repository":
		outputs["name"] = args.Name
		// Expected outputs: name, repositoryId, location, project, format
	case "gcp:serviceaccount/account:Account":
		outputs["name"] = args.Name
		outputs["email"] = args.Name + "@test-project.iam.gserviceaccount.com"
		// Expected outputs: name, accountId, project, displayName, email
	case "gcp:iam/workloadIdentityPool:WorkloadIdentityPool":
		outputs["name"] = args.Name
		// Expected outputs: name, workloadIdentityPoolId, project, displayName, description, disabled
	case "gcp:iam/workloadIdentityPoolProvider:WorkloadIdentityPoolProvider":
		outputs["name"] = args.Name
		// Expected outputs: name, workloadIdentityPoolProviderId, project, displayName, description, disabled, attributeMapping, attributeCondition, oidc
	case "gcp:serviceaccount/iAMMember:IAMMember":
		// Expected outputs: role, member, serviceAccountId
	case "gcp:artifactregistry/repositoryIamMember:RepositoryIamMember":
		// Expected outputs: role, member, repository
	case "gcp:projects/iAMMember:IAMMember":
		// Expected outputs: role, member, project
	case "gcp:storage/bucket:Bucket":
		outputs["name"] = args.Name
		outputs["uniformBucketLevelAccess"] = true
		// Expected outputs: name, location, project, versioning, lifecycleRules, labels, uniformBucketLevelAccess
	case "gcp:storage/bucketIAMMember:BucketIAMMember":
		// Expected outputs: bucket, role, member
	case "gcp:organizations/project:Project":
		outputs["name"] = args.Name
		outputs["number"] = "123456789012" // Numeric project ID - used in workload identity provider ID
		// Expected outputs: name, projectId, number, autoCreateNetwork
	}

	return args.Name + "_id", resource.NewPropertyMapFromMap(outputs), nil
}

func (m *infraMocks) Call(_ pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func TestNewGithubGoogleRegistry(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		config := &ci.Config{
			GCPProject:               "test-project",
			GCPRegion:                "us-central1",
			RepositoryLocation:       "us",
			ResourcePrefix:           "ci-with-a-long-prefix",
			RepositoryName:           "registry",
			AllowedRepoURL:           "https://github.com/test/repo",
			IdentityPoolProviderName: "github-actions-provider",
			RepositoryOwner:          "test",
			RepositoryOwnerID:        "1234567890",
			RepositoryID:             "1234567890",
		}

		infra, err := ci.NewGithubGoogleRegistry(ctx, config)
		require.NoError(t, err)

		// 1. Workload identity pool config

		// Note: Pulumi Output types are asynchronous and lazy-evaluated.
		// We use channels to synchronously extract values in test context.
		// When ApplyT() is called, it schedules the function to run when the output becomes available.
		// The channel allows us to wait for and capture the actual value for assertions.
		attrMapCh := make(chan map[string]string, 1)

		infra.OidcProvider.AttributeMapping.ToStringMapOutput().ApplyT(func(m map[string]string) map[string]string {
			attrMapCh <- m

			return m
		})

		attrMap := <-attrMapCh
		assert.Equal(t, "assertion.sub", attrMap["google.subject"])
		assert.Equal(t, "assertion.repository", attrMap["attribute.repository"])
		assert.Equal(t, "assertion.actor", attrMap["attribute.actor"])
		assert.Equal(t, "assertion.aud", attrMap["attribute.aud"])

		condCh := make(chan *string, 1)

		infra.OidcProvider.AttributeCondition.ApplyT(func(cond *string) *string {
			condCh <- cond

			return cond
		})

		cond := <-condCh
		if cond != nil {
			assert.Contains(t, *cond, "attribute.repository == \"test/repo\"")
			assert.Contains(t, *cond, "attribute.repository_owner == \"test\"")
			assert.Contains(t, *cond, "attribute.repository_owner_id == \"1234567890\"")
			assert.Contains(t, *cond, "attribute.repository_id == \"1234567890\"")
		} else {
			assert.Fail(t, "AttributeCondition should not be nil")
		}

		issuerCh := make(chan *string, 1)

		infra.OidcProvider.Oidc.IssuerUri().ApplyT(func(uri *string) *string {
			issuerCh <- uri

			return uri
		})

		issuer := <-issuerCh
		if issuer != nil {
			assert.Equal(t, "https://token.actions.githubusercontent.com", *issuer)
		} else {
			assert.Fail(t, "IssuerUri should not be nil")
		}

		// 2. Repository write access

		regURLCh := make(chan string, 1)

		infra.RegistryURL.ApplyT(func(url string) string {
			regURLCh <- url

			return url
		})

		regURL := <-regURLCh
		assert.Contains(t, regURL, "us-docker.pkg.dev/test-project/ci-with-a-long-prefix-registry")

		// 3. Resource name length constraint

		nameCh := make(chan string, 1)

		infra.OidcProvider.Name.ApplyT(func(name string) string {
			nameCh <- name

			return name
		})

		name := <-nameCh
		assert.LessOrEqual(t, len(name), 32)

		// 4. Principal IAM bindings

		assert.NotNil(t, infra.RepositoryPrincipalID)
		assert.NotNil(t, infra.RepositoryIAMMembers)
		assert.NotNil(t, infra.ProjectIAMMembers)

		principalCh := make(chan string, 1)

		infra.RepositoryPrincipalID.ApplyT(func(principal string) string {
			principalCh <- principal

			return principal
		})

		principal := <-principalCh
		assert.Equal(t, principal, "principalSet://iam.googleapis.com/ci-with-a-long-prefix-github-act/attribute.repository/test/repo")

		// ------- Repository-level IAM -------

		memberCh := make(chan string, 1)

		infra.RepositoryIAMMembers[0].Member.ApplyT(func(member string) string {
			memberCh <- member

			return member
		})

		firstMember := <-memberCh
		assert.Equal(t, firstMember, "principalSet://iam.googleapis.com/ci-with-a-long-prefix-github-act/attribute.repository/test/repo")

		roleCh := make(chan string, 1)

		infra.RepositoryIAMMembers[0].Role.ApplyT(func(role string) string {
			roleCh <- role

			return role
		})

		firstRole := <-roleCh
		assert.Equal(t, firstRole, "roles/artifactregistry.writer")

		// ------- Project-level IAM -------

		infra.ProjectIAMMembers[0].Role.ApplyT(func(role string) string {
			roleCh <- role

			return role
		})

		// assert SBOM generation roles
		projectRole := <-roleCh
		assert.Equal(t, projectRole, "roles/containeranalysis.notes.editor")

		infra.ProjectIAMMembers[1].Role.ApplyT(func(role string) string {
			roleCh <- role

			return role
		})

		secondProjectRole := <-roleCh
		assert.Equal(t, secondProjectRole, "roles/containeranalysis.occurrences.editor")

		// 5. SBOM bucket creation and IAM

		// Test that SBOM bucket is created with expected default name
		assert.NotNil(t, infra.SBOMBucket)
		assert.NotNil(t, infra.SBOMBucketIAMMember)

		bucketNameCh := make(chan string, 1)

		infra.SBOMBucket.Name.ApplyT(func(name string) string {
			bucketNameCh <- name

			return name
		})

		bucketName := <-bucketNameCh
		assert.Equal(t, "artifacts-test-project-sbom", bucketName)

		// Test that bucket IAM member has correct role
		bucketRoleCh := make(chan string, 1)

		infra.SBOMBucketIAMMember.Role.ApplyT(func(role string) string {
			bucketRoleCh <- role

			return role
		})

		bucketRole := <-bucketRoleCh
		assert.Equal(t, "roles/storage.objectAdmin", bucketRole)

		// Test that bucket IAM member has correct principal
		bucketMemberCh := make(chan string, 1)

		infra.SBOMBucketIAMMember.Member.ApplyT(func(member string) string {
			bucketMemberCh <- member

			return member
		})

		bucketMember := <-bucketMemberCh
		assert.Equal(t, "principalSet://iam.googleapis.com/ci-with-a-long-prefix-github-act/attribute.repository/test/repo", bucketMember)

		// Uniform Bucket Level Access is required for SBOMs
		ublaCh := make(chan bool, 1)

		infra.SBOMBucket.UniformBucketLevelAccess.ApplyT(func(ubla bool) bool {
			ublaCh <- ubla

			return ubla
		})

		ubla := <-ublaCh
		assert.True(t, ubla, "Uniform Bucket Level Access should be enabled for SBOM buckets")

		// 6. Workload Identity Pool Provider ID

		// Test that WorkloadIdentityPoolProviderID is set with numeric project ID
		assert.NotNil(t, infra.WorkloadIdentityPoolProviderID)

		providerIDCh := make(chan string, 1)

		infra.WorkloadIdentityPoolProviderID.ApplyT(func(id string) string {
			providerIDCh <- id

			return id
		})

		providerID := <-providerIDCh
		// Should use numeric project ID (123456789012) not project name (test-project)
		assert.Contains(t, providerID, "projects/123456789012/locations/global/workloadIdentityPools/")
		// Both pool and provider names get truncated to 32 chars: "ci-with-a-long-prefix-github-act"
		assert.Contains(t, providerID, "/workloadIdentityPools/ci-with-a-long-prefix-github-act/providers/ci-with-a-long-prefix-github-act")

		return nil
	}, pulumi.WithMocks("project", "stack", &infraMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}
