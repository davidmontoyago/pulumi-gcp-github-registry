// Package ci contains the infra required to setup a Github Actions pipeline with secure access to GCP
package ci

import (
	"fmt"
	"log"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all the configuration from environment variables
type Config struct {
	GCPProject               string `envconfig:"GCP_PROJECT" required:"true"`
	GCPRegion                string `envconfig:"GCP_REGION" required:"true"`
	ResourcePrefix           string `envconfig:"RESOURCE_PREFIX" default:"ci"`
	RepositoryName           string `envconfig:"REPOSITORY_NAME" default:"registry"`
	AllowedRepoURL           string `envconfig:"ALLOWED_REPO_URL" default:"https://github.com/davidmontoyago/pulumi-gcp-github-registry"`
	IdentityPoolProviderName string `envconfig:"IDENTITY_POOL_PROVIDER_NAME" default:"github-actions-provider"`
}

// LoadConfig loads configuration from environment variables
// All environment variables are required and will cause an error if not set
func LoadConfig() (*Config, error) {
	var config Config

	err := envconfig.Process("", &config)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration from environment variables: %w", err)
	}

	log.Printf("Configuration loaded successfully:")
	log.Printf("  GCP Project: %s", config.GCPProject)
	log.Printf("  GCP Region: %s", config.GCPRegion)
	log.Printf("  Resource Prefix: %s", config.ResourcePrefix)
	log.Printf("  Repository Name: %s", config.RepositoryName)
	log.Printf("  Allowed Repo URL: %s", config.AllowedRepoURL)
	log.Printf("  Identity Pool Provider Name: %s", config.IdentityPoolProviderName)

	return &config, nil
}
