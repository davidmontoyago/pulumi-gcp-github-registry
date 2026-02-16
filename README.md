# pulumi-gcp-github-registry

[![Develop](https://github.com/davidmontoyago/pulumi-gcp-github-registry/workflows/develop/badge.svg)](https://github.com/davidmontoyago/pulumi-gcp-github-registry/actions?query=workflow%3Adevelop) [![Go Coverage](https://raw.githubusercontent.com/wiki/davidmontoyago/pulumi-gcp-github-registry/coverage.svg)](https://raw.githack.com/wiki/davidmontoyago/pulumi-gcp-github-registry/coverage.html) [![Go Reference](https://pkg.go.dev/badge/github.com/davidmontoyago/pulumi-gcp-github-registry.svg)](https://pkg.go.dev/github.com/davidmontoyago/pulumi-gcp-github-registry)

Pulumi Component to setup an artifact registry repository, an OIDC identity provider for Github Actions, and the IAM required to login and push docker images to the registry.

Favors [Direct Workload Identity Federation](https://github.com/google-github-actions/auth/blob/v2.1.10/README.md#preferred-direct-workload-identity-federation) for Github Actions, but supports [Workload Identity Federation through a Service Account](https://github.com/google-github-actions/auth/blob/v2.1.10/README.md#workload-identity-federation-through-a-service-account) (`CREATE_SERVICE_ACCOUNT=true`) for cases when a GSA is required. Both approaches avoid long-lived access credentials. E.g.:

```yaml
- name: Authenticate to Google Cloud
  uses: google-github-actions/auth@v2
  with:
    project_id: ${{ env.GCP_PROJECT }}
    workload_identity_provider: ${{ env.WORKLOAD_IDENTITY_PROVIDER }}
```

See:
- https://github.com/google-github-actions/auth/blob/v2.1.10/README.md#setup
- https://github.com/google-github-actions/auth/pull/348
- https://github.com/docker/login-action/issues/640
- https://github.com/docker/login-action?tab=readme-ov-file#google-artifact-registry-gar

## Features

1. **Artifact Registry Repository**
   - Docker image storage for CI/CD builds
   - Configured with appropriate IAM permissions
   - Region-specific or multi-region deployment
   - Automatic image cleanup policy

2. **Workload Identity Federation**
   - OIDC-based authentication for GitHub Actions
   - Secure token exchange without long-lived credentials
   - Attribute mapping for repository and actor-based access control

3. **IAM Integration**
   - Automatic permission assignment for Artifact Registry access
   - Optional service account and binding to workload identity pool
   - Configurable role assignments

4. **SBOM Storage Bucket**
   - Dedicated Google Cloud Storage bucket for Software Bill of Materials (SBOMs)
   - Versioning enabled for audit trail and compliance
   - Lifecycle management (1-year retention policy)
   - Uniform Bucket Level Access (UBLA) for enhanced security

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
        ciInfra, err := ci.NewGithubGoogleRegistry(ctx, config)
        if err != nil {
            return err
        }

        // Export outputs for GitHub Actions
        ctx.Export("registryURL", ciInfra.RegistryURL)
        ctx.Export("sbomBucketName", ciInfra.SBOMBucket.Name)
        ctx.Export("workloadIdentityProviderID", pulumi.ToSecret(ciInfra.OidcProvider.ID()))
        ctx.Export("repositoryWorkloadID", ciInfra.RepositoryPrincipalID)

        return nil
    })
}
```

## Configuration

The component uses environment variables for configuration:

| Variable                       | Description                                                    | Required | Default                                                        |
| ------------------------------ | -------------------------------------------------------------- | -------- | -------------------------------------------------------------- |
| `GCP_PROJECT`                  | GCP Project ID                                                 | Yes      | -                                                              |
| `GCP_REGION`                   | GCP Region for resources                                       | Yes      | -                                                              |
| `REPOSITORY_LOCATION`          | Artifact Registry location                                     | No       | Value of `GCP_REGION`                                          |
| `ALLOWED_REPO_URL`             | GitHub repository URL for workload identity access             | No       | `https://github.com/davidmontoyago/pulumi-gcp-github-registry` |
| `REPOSITORY_OWNER`             | GitHub repository owner (username/org) for additional security | No       | -                                                              |
| `REPOSITORY_OWNER_ID`          | GitHub repository owner numeric ID (recommended for security)  | No       | -                                                              |
| `REPOSITORY_ID`                | GitHub repository numeric ID (recommended for security)        | No       | -                                                              |
| `IDENTITY_POOL_PROVIDER_NAME`  | Workload identity pool provider name (max 32 chars)            | No       | `github-actions-provider`                                      |
| `RESOURCE_PREFIX`              | Prefix for resource names                                      | No       | `ci`                                                           |
| `REPOSITORY_NAME`              | Artifact Registry repository name                              | No       | `registry`                                                     |
| `CREATE_SERVICE_ACCOUNT`       | Whether to create a GitHub Actions service account             | No       | `false`                                                        |
| `RECENT_IMAGE_RETENTION_COUNT` | Number of recent images to retain                              | No       | `10`                                                           |
| `OLD_IMAGE_DELETION_DAYS`      | Duration after which old images are deleted (e.g. `30d`)       | No       | `30d`                                                          |
| `SBOM_RETENTION_DAYS`          | Number of days after which SBOMs are deleted                   | No       | `365`                                                          |

## GitHub Actions Integration

### Setting up Workload Identity Federation

1. **Configure GitHub Actions Secrets**

Add the following secrets to your GitHub repository:

```yaml
# .github/workflows/deploy.yml
env:
  GCP_PROJECT: ${{ secrets.GCP_PROJECT }}
  WORKLOAD_IDENTITY_PROVIDER: ${{ secrets.WORKLOAD_IDENTITY_PROVIDER }}
```

2. **Authenticate with GCP**

Use the `google-github-actions/auth` action to authenticate:

```yaml
- name: Google Auth
  id: auth
  uses: google-github-actions/auth@v2
  with:
    project_id: ${{ env.GCP_PROJECT }}
    workload_identity_provider: ${{ env.WORKLOAD_IDENTITY_PROVIDER }}

- name: Login to Google Artifact Registry
  uses: docker/login-action@v3
  with:
    registry: ${{ env.REGISTRY_URL}}
    username: oauth2accesstoken
    password: ${{ steps.auth.outputs.auth_token }}

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
      id: auth
      uses: google-github-actions/auth@v2
      with:
        project_id: ${{ env.GCP_PROJECT }}
        workload_identity_provider: ${{ env.WORKLOAD_IDENTITY_PROVIDER }}

    # optionally get setup for gcloud commands
    - name: Set up Cloud SDK
      uses: google-github-actions/setup-gcloud@v2

    - name: Login to Google Artifact Registry
      uses: docker/login-action@v3
      with:
        registry: ${{ env.REGISTRY_URL }}
        username: oauth2accesstoken
        password: ${{ steps.auth.outputs.auth_token }}

    - name: Build and Push Image
      run: |
        docker build -t ${{ env.REGISTRY_URL }}/app:${{ github.sha }} .
        docker push ${{ env.REGISTRY_URL }}/app:${{ github.sha }}
```

## SBOM Storage and Analysis

This component creates a dedicated GCS bucket for storing Software Bill of Materials (SBOMs) generated for every image. This enables compliance, security scanning, and vulnerability management for container images.

### SBOM Bucket Features

- **Automatic Creation**: A bucket named `artifacts-{project-id}-sbom` is created automatically
- **Secure Access**: GitHub Actions workflows can upload SBOMs using the same workload identity federation
- **Versioning**: All SBOMs are versioned for audit trail and compliance requirements
- **Lifecycle Management**: SBOMs are automatically deleted after 1 year to manage storage costs

### Generating SBOMs in Github Actions

```yaml
- name: Generate SBOM with Syft
  uses: anchore/sbom-action@v0.19.0
  with:
    image: ${{ env.REGISTRY_URL }}/my-image:${{ github.sha }}
    output-file: sbom.spdx.json
    format: spdx-json
- name: Upload SBOM to GAR
  shell: bash
    gcloud artifacts sbom load \
      --source=sbom.spdx.json \
      --destination=gs://artifacts-my-project-sbom \
      --uri=${{ env.REGISTRY_URL }}/my-image:${{ github.sha }}
```

### Container Analysis Integration

The component automatically grants the necessary IAM permissions for Google Cloud's Container Analysis service:

- `roles/containeranalysis.notes.editor`: Create and manage vulnerability notes
- `roles/containeranalysis.occurrences.editor`: Create and manage vulnerability occurrences
- `roles/storage.bucketViewer`: Access to view bucket contents for analysis

This enables integration with Google Cloud's vulnerability scanning and compliance tools.

## Security Features

- **Workload Identity Federation**: Eliminates the need for long-lived service account keys
- **Least Privilege Access**: Service account has minimal required permissions
- **Repository Scoping**: OIDC provider can be configured to restrict access to specific repositories
- **Repository Owner Constraints**: Additional security through owner username and numeric ID validation
- **Audit Logging**: All operations are logged in GCP Cloud Audit Logs

## Repository Scoping

This component implements security best practices for Workload Identity Federation by restricting OIDC authentication to specific GitHub repositories.

See:
- https://github.com/google-github-actions/auth/blob/v2.1.10/docs/SECURITY_CONSIDERATIONS.md

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

| GitHub Attribute                | GCP Attribute                   | Description                            |
| ------------------------------- | ------------------------------- | -------------------------------------- |
| `assertion.sub`                 | `google.subject`                | Unique identifier for the workflow run |
| `assertion.actor`               | `attribute.actor`               | GitHub username of the actor           |
| `assertion.repository`          | `attribute.repository`          | Repository name (e.g., `owner/repo`)   |
| `assertion.repository_owner`    | `attribute.repository_owner`    | Repository owner (username/org)        |
| `assertion.repository_owner_id` | `attribute.repository_owner_id` | Repository owner numeric ID            |
| `assertion.repository_id`       | `attribute.repository_id`       | Repository numeric ID                  |
| `assertion.ref`                 | `attribute.ref`                 | Branch or tag reference                |
| `assertion.sha`                 | `attribute.sha`                 | Commit SHA                             |
| `assertion.workflow`            | `attribute.workflow`            | Workflow name                          |
| `assertion.head_ref`            | `attribute.head_ref`            | PR head reference                      |
| `assertion.base_ref`            | `attribute.base_ref`            | PR base reference                      |

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
- `sbomBucketName`: The name of the GCS bucket for SBOM storage
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
    sbomBucketName             artifacts-my-project-sbom

$ pulumi stack output --show-secrets
Current stack outputs (1):
    OUTPUTS
    workloadIdentityPoolID     projects/123456789/locations/global/workloadIdentityPools/ci-github-actions-pool
    workloadIdentityProviderID projects/123456789/locations/global/workloadIdentityPools/ci-github-actions-pool/providers/ci-github-actions-provider
    registryURL                us-docker.pkg.dev/my-project/registry
    sbomBucketName             artifacts-my-project-sbom
```
