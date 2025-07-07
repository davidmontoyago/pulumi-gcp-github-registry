# pulumi-gcp-github-registry

Pulumi Component to setup the CI/CD infrastructure required for GitHub Actions to access GCP with OIDC, and push images to Artifact Registry.

This setup uses Workload [Identity Federation through a Service Account](https://github.com/google-github-actions/auth/blob/v2.1.10/README.md#workload-identity-federation-through-a-service-account) to allow Github Actions to access GCP without long-lived credentials.

See:
- https://github.com/google-github-actions/auth/blob/v2.1.10/README.md#setup
- https://github.com/google-github-actions/auth/pull/348
- https://github.com/google-github-actions/auth/blob/v2.1.10/docs/SECURITY_CONSIDERATIONS.md

## Overview

This component creates the necessary GCP infrastructure to support GitHub Actions CI/CD pipelines with secure authentication and container image management.

## Features

1. **Artifact Registry Repository**
   - Docker image storage for CI/CD builds
   - Configured with appropriate IAM permissions
   - Region-specific deployment

2. **GitHub Actions Service Account**
   - Dedicated service account for CI/CD operations
   - Minimal required permissions following security best practices
   - Clear naming and description for easy identification

3. **Workload Identity Federation**
   - OIDC-based authentication for GitHub Actions
   - Secure token exchange without long-lived credentials
   - Attribute mapping for repository and actor-based access control

4. **IAM Integration**
   - Automatic permission assignment for Artifact Registry access
   - Service account binding to workload identity pool
   - Configurable role assignments

## Install

```bash
go get github.com/davidmontoyago/pulumi-gcp-github-registry
```

## Quickstart

```go
package main

import (
    "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
    "github.com/davidmontoyago/pulumi-gcp-github-registry/deploy/ci"
)

func main() {
    pulumi.Run(func(ctx *pulumi.Context) error {
        // Load configuration from environment variables
        config, err := ci.LoadConfig()
        if err != nil {
            return err
        }

        // Create CI/CD infrastructure
        ciInfra, err := ci.NewGithubGoogleRegistryStack(ctx, config)
        if err != nil {
            return err
        }

        // Export outputs for GitHub Actions
        ctx.Export("registryURL", ciInfra.RegistryUrl)
        ctx.Export("serviceAccountEmail", ciInfra.GitHubActionsServiceAccount.Email)
        ctx.Export("workloadIdentityPoolID", pulumi.ToSecret(ciInfra.WorkloadIdentityPool.ID()))
        ctx.Export("workloadIdentityProviderID", pulumi.ToSecret(ciInfra.OidcProvider.ID()))

        return nil
    })
}
```

## Configuration

The component uses environment variables for configuration:

| Variable                      | Description                                         | Required | Default                                                        |
| ----------------------------- | --------------------------------------------------- | -------- | -------------------------------------------------------------- |
| `GCP_PROJECT`                 | GCP Project ID                                      | Yes      | -                                                              |
| `GCP_REGION`                  | GCP Region for resources                            | Yes      | -                                                              |
| `RESOURCE_PREFIX`             | Prefix for resource names                           | No       | `ci`                                                           |
| `REPOSITORY_NAME`             | Artifact Registry repository name                   | No       | `registry`                                                     |
| `ALLOWED_REPO_URL`            | GitHub repository URL for workload identity access  | No       | `https://github.com/davidmontoyago/pulumi-gcp-github-registry` |
| `IDENTITY_POOL_PROVIDER_NAME` | Workload identity pool provider name (max 32 chars) | No       | `github-actions-provider`                                      |

## GitHub Actions Integration

### Setting up Workload Identity Federation

1. **Configure GitHub Actions Secrets**

Add the following secrets to your GitHub repository:

```yaml
# .github/workflows/deploy.yml
env:
  GCP_PROJECT: ${{ secrets.GCP_PROJECT }}
  WORKLOAD_IDENTITY_PROVIDER: ${{ secrets.WORKLOAD_IDENTITY_PROVIDER }}
  SERVICE_ACCOUNT_EMAIL: ${{ secrets.SERVICE_ACCOUNT_EMAIL }}
```

2. **Authenticate with GCP**

Use the `google-github-actions/auth` action to authenticate:

```yaml
- name: Authenticate to Google Cloud
  uses: google-github-actions/auth@v2
  with:
    token_format: "access_token"
    project_id: ${{ inputs.gcp-project }}
    workload_identity_provider: ${{ env.WORKLOAD_IDENTITY_PROVIDER }}
    service_account: ${{ env.SERVICE_ACCOUNT_EMAIL }}

- name: Set up Cloud SDK
  uses: google-github-actions/setup-gcloud@v2

- name: Login to Google Artifact Registry
  uses: docker/login-action@v3
  with:
    registry: ${{ inputs.registry-url }}
    username: oauth2accesstoken
    password: ${{ steps.auth.outputs.access_token }}

- name: Build Docker image
  shell: bash
  run: make image
```


### Complete GitHub Actions Workflow Example

```yaml
name: Deploy to GCP

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  GCP_PROJECT: ${{ secrets.GCP_PROJECT }}
  WORKLOAD_IDENTITY_PROVIDER: ${{ secrets.WORKLOAD_IDENTITY_PROVIDER }}
  SERVICE_ACCOUNT_EMAIL: ${{ secrets.SERVICE_ACCOUNT_EMAIL }}
  REGISTRY_URL: ${{ secrets.REGISTRY_URL }}

jobs:
  deploy:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      id-token: write

    steps:
    - uses: actions/checkout@v4

    - name: Google Auth
      uses: google-github-actions/auth@v2
      with:
        token_format: "access_token"
        project_id: ${{ inputs.gcp-project }}
        workload_identity_provider: ${{ env.WORKLOAD_IDENTITY_PROVIDER }}
        service_account: ${{ env.SERVICE_ACCOUNT_EMAIL }}

    - name: Set up Cloud SDK
      uses: google-github-actions/setup-gcloud@v2

    - name: Login to Google Artifact Registry
      uses: docker/login-action@v3
      with:
        registry: ${{ inputs.registry-url }}
        username: oauth2accesstoken
        password: ${{ steps.auth.outputs.access_token }}

    - name: Build and Push Image
      run: |
        docker build -t ${{ env.REGISTRY_URL }}/app:${{ github.sha }} .
        docker push ${{ env.REGISTRY_URL }}/app:${{ github.sha }}
```

## Security Features

- **Workload Identity Federation**: Eliminates the need for long-lived service account keys
- **Least Privilege Access**: Service account has minimal required permissions
- **Repository Scoping**: OIDC provider can be configured to restrict access to specific repositories
- **Audit Logging**: All operations are logged in GCP Cloud Audit Logs

## Repository Scoping

This component implements security best practices for Workload Identity Federation by restricting OIDC authentication to specific GitHub repositories.

### Security Configuration

The OIDC provider is configured with multiple security layers:

#### 1. **Repository-Specific Access Control**
```go
AttributeCondition: pulumi.String("attribute.repository == \"my-org/my-repo\""),
```
This ensures that **only the specified repository** can authenticate with the workload identity pool. Any attempt from other repositories will be rejected.

#### 2. **Audience Validation**

The token audience claim will be validated in GCP against the full name of the OIDC pool provider.

See:
- https://github.com/pulumi/pulumi-gcp/blob/736e673d396ae824600e554d34191bea84289e45/sdk/go/gcp/iam/pulumiTypes.go#L2478-L2498

#### 3. **Comprehensive Attribute Mapping**
The provider maps GitHub Actions context to GCP attributes for fine-grained control:

| GitHub Attribute       | GCP Attribute          | Description                            |
| ---------------------- | ---------------------- | -------------------------------------- |
| `assertion.sub`        | `google.subject`       | Unique identifier for the workflow run |
| `assertion.actor`      | `attribute.actor`      | GitHub username of the actor           |
| `assertion.repository` | `attribute.repository` | Repository name (e.g., `owner/repo`)   |
| `assertion.ref`        | `attribute.ref`        | Branch or tag reference                |
| `assertion.sha`        | `attribute.sha`        | Commit SHA                             |
| `assertion.workflow`   | `attribute.workflow`   | Workflow name                          |
| `assertion.head_ref`   | `attribute.head_ref`   | PR head reference                      |
| `assertion.base_ref`   | `attribute.base_ref`   | PR base reference                      |

### Security Benefits

- **Repository Isolation**: Prevents cross-repository access
- **Audit Trail**: All authentication attempts are logged with repository context
- **No Credential Exposure**: Eliminates the risk of leaked service account keys
- **Automatic Rotation**: GitHub Actions tokens are automatically rotated
- **Least Privilege**: Service account only has necessary permissions

### Verification

You can verify the repository scoping is working by:

1. **Checking GCP Cloud Audit Logs** for authentication events
2. **Testing from unauthorized repositories** (should be rejected)
3. **Monitoring the `google.subject` attribute** in logs to ensure proper mapping

## Outputs

The component exports the following values for use in CI/CD pipelines:

- `registryURL`: The full URL of the Artifact Registry repository
- `serviceAccountEmail`: The email of the GitHub Actions service account
- `workloadIdentityPoolID`: The ID of the workload identity pool **(marked as secret)**
- `workloadIdentityProviderID`: The full provider ID for GitHub Actions authentication **(marked as secret)**
- `workloadIdentityProviderCondition`: The attribute condition used for repository scoping

### Security Note for Exported Values

The `workloadIdentityPoolId` and `workloadIdentityProviderId` are marked as **secrets** in Pulumi state:

- **Encrypted in state**: These values are encrypted when stored in Pulumi state files
- **Masked in logs**: Values are displayed as `[secret]` in Pulumi CLI output
- **Secure retrieval**: Use `pulumi stack output --show-secrets` to view the actual values

**Example output:**
```bash
$ pulumi stack output
Current stack outputs (1):
    OUTPUTS
    workloadIdentityPoolID     [secret]
    workloadIdentityProviderID [secret]
    registryURL                us-docker.pkg.dev/my-project/registry
    serviceAccountEmail        ci-github-actions@my-project.iam.gserviceaccount.com
    workloadIdentityProviderCondition attribute.repository == "davidmontoyago/pulumi-gcp-github-registry"

$ pulumi stack output --show-secrets
Current stack outputs (1):
    OUTPUTS
    workloadIdentityPoolID     projects/123456789/locations/global/workloadIdentityPools/ci-github-actions-pool
    workloadIdentityProviderID projects/123456789/locations/global/workloadIdentityPools/ci-github-actions-pool/providers/ci-github-actions-provider
    registryURL                us-docker.pkg.dev/my-project/registry
    serviceAccountEmail        ci-github-actions@my-project.iam.gserviceaccount.com
    workloadIdentityProviderCondition attribute.repository == "davidmontoyago/pulumi-gcp-github-registry"
```
