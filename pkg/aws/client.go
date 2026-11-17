package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cradle-dev/cradle/pkg/domain"
)

// ClientSet bundles initialized AWS service clients for an assumed cross-account session in a region.
type ClientSet struct {
	Region    string
	AccountID string
	Config    aws.Config

	EC2          *ec2.Client
	S3           *s3.Client
	RDS          *rds.Client
	Lambda       *lambda.Client
	ECS          *ecs.Client
	EKS          *eks.Client
	ELB          *elasticloadbalancingv2.Client
	STS          *sts.Client
	CloudWatch   *cloudwatch.Client
	CostExplorer *costexplorer.Client
}

// ClientFactory manages AWS cross-account credentials and clients.
type ClientFactory struct {
	baseCfg aws.Config
	limiter *TokenBucket
}

// NewClientFactory creates a factory with adaptive rate limiting and jittered retries.
func NewClientFactory(ctx context.Context) (*ClientFactory, error) {
	// Custom retry policy: standard AWS backoff with full jitter and up to 5 attempts
	retryer := retry.NewStandard(func(o *retry.StandardOptions) {
		o.MaxAttempts = 5
		o.Backoff = retry.NewExponentialJitterBackoff(20 * time.Second)
	})

	baseCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRetryer(func() aws.Retryer { return retryer }),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load default AWS config: %w", err)
	}

	return &ClientFactory{
		baseCfg: baseCfg,
		limiter: NewTokenBucket(50, 15), // 50 burst, 15 tokens/sec
	}, nil
}

// AssumeRoleClientSet creates a regional ClientSet by assuming the customer's cross-account IAM role.
func (f *ClientFactory) AssumeRoleClientSet(ctx context.Context, account *domain.CloudAccount, region string) (*ClientSet, error) {
	if err := account.Validate(); err != nil {
		return nil, fmt.Errorf("invalid cloud account: %w", err)
	}
	if region == "" {
		region = "us-east-1"
	}

	// 1. Base STS client using Cradle's hosting credentials
	baseSTS := sts.NewFromConfig(f.baseCfg, func(o *sts.Options) {
		o.Region = region
	})

	// 2. Setup AssumeRole credential provider with ExternalId
	sessionName := fmt.Sprintf("cradle-%s-%d", account.TenantID.String()[:8], time.Now().Unix())
	creds := stscreds.NewAssumeRoleProvider(baseSTS, account.RoleARN, func(o *stscreds.AssumeRoleOptions) {
		o.ExternalID = aws.String(account.ExternalID)
		o.RoleSessionName = sessionName
		o.Duration = 1 * time.Hour
	})

	// 3. Regional assumed role AWS config
	regionalCfg := f.baseCfg.Copy()
	regionalCfg.Region = region
	regionalCfg.Credentials = aws.NewCredentialsCache(creds)

	return &ClientSet{
		Region:       region,
		AccountID:    account.AccountID,
		Config:       regionalCfg,
		EC2:          ec2.NewFromConfig(regionalCfg),
		S3:           s3.NewFromConfig(regionalCfg),
		RDS:          rds.NewFromConfig(regionalCfg),
		Lambda:       lambda.NewFromConfig(regionalCfg),
		ECS:          ecs.NewFromConfig(regionalCfg),
		EKS:          eks.NewFromConfig(regionalCfg),
		ELB:          elasticloadbalancingv2.NewFromConfig(regionalCfg),
		STS:          sts.NewFromConfig(regionalCfg),
		CloudWatch:   cloudwatch.NewFromConfig(regionalCfg),
		CostExplorer: costexplorer.NewFromConfig(regionalCfg),
	}, nil
}
