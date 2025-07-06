package ci_test

import (
	"testing"

	"github.com/davidmontoyago/pulumi-gcp-github-registry/deploy/ci"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockPulumiContext provides a mock context for testing
type MockPulumiContext struct {
	*pulumi.Context
	exports map[string]interface{}
}

func NewMockPulumiContext() *MockPulumiContext {
	return &MockPulumiContext{
		exports: make(map[string]interface{}),
	}
}

func (m *MockPulumiContext) Export(name string, value interface{}) {
	m.exports[name] = value
}

type infraMocks struct{}

func (m *infraMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	outputs := map[string]interface{}{}
	for k, v := range args.Inputs {
		outputs[string(k)] = v // resource.PropertyKey to string
	}
	// Simulate IDs and outputs for each resource type
	switch args.TypeToken {
	case "gcp:artifactregistry/repository:Repository":
		outputs["name"] = args.Name
		outputs["repositoryId"] = outputs["repositoryId"]
		outputs["location"] = outputs["location"]
		outputs["project"] = outputs["project"]
		outputs["format"] = outputs["format"]
	case "gcp:serviceaccount/account:Account":
		outputs["name"] = args.Name
		outputs["accountId"] = outputs["accountId"]
		outputs["project"] = outputs["project"]
		outputs["displayName"] = outputs["displayName"]
		outputs["email"] = args.Name + "@test-project.iam.gserviceaccount.com"
	case "gcp:iam/workloadIdentityPool:WorkloadIdentityPool":
		outputs["name"] = args.Name
		outputs["workloadIdentityPoolId"] = outputs["workloadIdentityPoolId"]
		outputs["project"] = outputs["project"]
		outputs["displayName"] = outputs["displayName"]
		outputs["description"] = outputs["description"]
		outputs["disabled"] = outputs["disabled"]
	case "gcp:iam/workloadIdentityPoolProvider:WorkloadIdentityPoolProvider":
		outputs["name"] = args.Name
		outputs["workloadIdentityPoolProviderId"] = outputs["workloadIdentityPoolProviderId"]
		outputs["project"] = outputs["project"]
		outputs["displayName"] = outputs["displayName"]
		outputs["description"] = outputs["description"]
		outputs["disabled"] = outputs["disabled"]
		outputs["attributeMapping"] = outputs["attributeMapping"]
		outputs["attributeCondition"] = outputs["attributeCondition"]
		outputs["oidc"] = outputs["oidc"]
	case "gcp:serviceaccount/iAMMember:IAMMember":
		outputs["role"] = outputs["role"]
		outputs["member"] = outputs["member"]
		outputs["serviceAccountId"] = outputs["serviceAccountId"]
	case "gcp:artifactregistry/repositoryIamMember:RepositoryIamMember":
		outputs["role"] = outputs["role"]
		outputs["member"] = outputs["member"]
		outputs["repository"] = outputs["repository"]
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
			ResourcePrefix:           "ci",
			RepositoryName:           "registry",
			AllowedRepoURL:           "https://github.com/test/repo",
			IdentityPoolProviderName: "github-actions-provider",
		}

		infra, err := ci.NewCiInfrastructure(ctx, config)
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
		assert.Equal(t, member, "principalSet://iam.googleapis.com/ci-github-actions-pool/attribute.repository/test/repo")

		return nil
	}, pulumi.WithMocks("project", "stack", &infraMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}
