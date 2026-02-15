// Package ci contains the infra required to setup a Github Actions pipeline with secure access to GCP
package ci

import (
	"fmt"
	"log"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all the configuration from environment variables
type Config struct {
	GCPProject string `envconfig:"GCP_PROJECT" required:"true"`
	// Supports both single region (e.g. us-central1, us-east1, etc.) and multi-region (e.g. us, europe, asia)
	GCPRegion string `envconfig:"GCP_REGION" required:"true"`
	// Repository location for Artifact Registry. Defaults to GCP_REGION but can be overridden for multi-region (e.g. us, europe, asia)
	RepositoryLocation string `envconfig:"REPOSITORY_LOCATION" default:""`
	AllowedRepoURL     string `envconfig:"ALLOWED_REPO_URL" default:"https://github.com/davidmontoyago/pulumi-gcp-github-registry"`
	// Repository owner (username or organization) for additional security constraints
	RepositoryOwner string `envconfig:"REPOSITORY_OWNER" default:""`
	// Repository owner numeric ID for additional security constraints (recommended)
	RepositoryOwnerID string `envconfig:"REPOSITORY_OWNER_ID" default:""`
	// Repository numeric ID for additional security constraints (recommended)
	RepositoryID             string `envconfig:"REPOSITORY_ID" default:""`
	IdentityPoolProviderName string `envconfig:"IDENTITY_POOL_PROVIDER_NAME" default:"github-actions-provider"`
	ResourcePrefix           string `envconfig:"RESOURCE_PREFIX" default:"ci"`
	RepositoryName           string `envconfig:"REPOSITORY_NAME" default:"registry"`
	CreateServiceAccount     bool   `envconfig:"CREATE_SERVICE_ACCOUNT" default:"false"`
	ProtectResources         bool   `envconfig:"PROTECT_RESOURCES" default:"false"`
	// Number of recent images to retain
	RecentImageRetentionCount int `envconfig:"RECENT_IMAGE_RETENTION_COUNT" default:"10"`
	// Number of days after which old images are deleted
	OldImageDeletionDays string `envconfig:"OLD_IMAGE_DELETION_DAYS" default:"30d"`
}

// LoadConfig loads configuration from environment variables
// All environment variables are required and will cause an error if not set
func LoadConfig() (*Config, error) {
	var config Config

	err := envconfig.Process("", &config)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration from environment variables: %w", err)
	}

	// Set default repository location to GCP region if not specified
	if config.RepositoryLocation == "" {
		config.RepositoryLocation = config.GCPRegion
	}

	log.Printf("Configuration loaded successfully:")
	log.Printf("  GCP Project: %s", config.GCPProject)
	log.Printf("  GCP Region: %s", config.GCPRegion)
	log.Printf("  Repository Location: %s", config.RepositoryLocation)
	log.Printf("  Resource Prefix: %s", config.ResourcePrefix)
	log.Printf("  Repository Name: %s", config.RepositoryName)
	log.Printf("  Allowed Repo URL: %s", config.AllowedRepoURL)
	log.Printf("  Protect Resources: %t", config.ProtectResources)
	log.Printf("  Recent Image Retention Count: %d", config.RecentImageRetentionCount)
	log.Printf("  Old Image Deletion Days: %d", config.OldImageDeletionDays)

	if config.RepositoryOwner != "" {
		log.Printf("  Repository Owner: %s", config.RepositoryOwner)
	}

	if config.RepositoryOwnerID != "" {
		log.Printf("  Repository Owner ID: %s", config.RepositoryOwnerID)
	}

	if config.RepositoryID != "" {
		log.Printf("  Repository ID: %s", config.RepositoryID)
	}

	log.Printf("  Identity Pool Provider Name: %s", config.IdentityPoolProviderName)

	return &config, nil
}
