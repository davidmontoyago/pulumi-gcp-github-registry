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
	}
	return args.Name + "_id", resource.NewPropertyMapFromMap(outputs), nil
}

func (m *infraMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func TestNewCiInfrastructure(t *testing.T) {
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		config := &ci.Config{
			GCPProject:               "test-project",
			GCPRegion:                "us-central1",
			ResourcePrefix:           "ci-with-a-long-prefix",
			RepositoryName:           "registry",
			AllowedRepoURL:           "https://github.com/test/repo",
			IdentityPoolProviderName: "github-actions-provider",
		}

		infra, err := ci.NewGithubGoogleRegistryStack(ctx, config)
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
			assert.Contains(t, *cond, "attribute.repository == ")
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

		regUrlCh := make(chan string, 1)
		infra.RegistryUrl.ApplyT(func(url string) string {
			regUrlCh <- url
			return url
		})
		regUrl := <-regUrlCh
		assert.Contains(t, regUrl, "us-docker.pkg.dev/test-project/registry")

		// 3. Resource name length constraint

		nameCh := make(chan string, 1)
		infra.OidcProvider.Name.ApplyT(func(name string) string {
			nameCh <- name
			return name
		})
		name := <-nameCh
		assert.LessOrEqual(t, len(name), 32)

		// 4. GSA IAM member binding

		assert.NotNil(t, infra.GitHubActionsServiceAccount)
		assert.NotNil(t, infra.ServiceAccountOidcMember)

		memberCh := make(chan string, 1)
		infra.ServiceAccountOidcMember.Member.ApplyT(func(member string) string {
			memberCh <- member
			return member
		})
		member := <-memberCh
		assert.Equal(t, member, "principalSet://iam.googleapis.com/ci-with-a-long-prefix-github-act/attribute.repository/test/repo")

		return nil
	}, pulumi.WithMocks("project", "stack", &infraMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}
