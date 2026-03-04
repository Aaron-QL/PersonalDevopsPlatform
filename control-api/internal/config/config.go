package config

import "github.com/kelseyhightower/envconfig"

type Config struct {
	MongoURI    string `envconfig:"MONGO_URI" default:"mongodb://localhost:27017"`
	MongoDB     string `envconfig:"MONGO_DB" default:"devops_platform"`
	ServerPort  int    `envconfig:"SERVER_PORT" default:"8080"`
	K8sInCluster bool  `envconfig:"K8S_IN_CLUSTER" default:"true"`

	GithubPAT string `envconfig:"GITHUB_PAT"`

	HarnessAPIKey       string `envconfig:"HARNESS_API_KEY"`
	HarnessAccountID    string `envconfig:"HARNESS_ACCOUNT_ID"`
	HarnessWebhookToken string `envconfig:"HARNESS_WEBHOOK_TOKEN"`

	EncryptionDEKSecret string `envconfig:"ENCRYPTION_DEK_SECRET" default:"control-api-dek"`
	BootstrapAdminKey   string `envconfig:"BOOTSTRAP_ADMIN_KEY"`

	AWSAccessKeyID     string `envconfig:"AWS_ACCESS_KEY_ID"`
	AWSSecretAccessKey string `envconfig:"AWS_SECRET_ACCESS_KEY"`
	AWSRegion          string `envconfig:"AWS_REGION" default:"ap-northeast-2"`
	TerraformWorkDir   string `envconfig:"TERRAFORM_WORKING_DIR" default:"../infrastructure/terraform"`

	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`
}

func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
