package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cradle-dev/cradle/pkg/aws/adapters"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/provider"
)

// AWSProvider implements provider.Provider for Amazon Web Services.
type AWSProvider struct {
	factory *ClientFactory
}

// NewAWSProvider initializes the AWS provider adapter.
func NewAWSProvider(factory *ClientFactory) *AWSProvider {
	return &AWSProvider{factory: factory}
}

func (p *AWSProvider) Name() domain.ProviderType {
	return domain.ProviderAWS
}

// VerifyConnection assumes the cross-account role and verifies caller identity and region access.
func (p *AWSProvider) VerifyConnection(ctx context.Context, account *domain.CloudAccount) (*provider.ConnectionVerificationResult, error) {
	clientSet, err := p.factory.AssumeRoleClientSet(ctx, account, "us-east-1")
	if err != nil {
		return nil, fmt.Errorf("failed assuming cross-account role: %w", err)
	}

	caller, err := clientSet.STS.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed sts:GetCallerIdentity: %w", err)
	}

	actualAccountID := aws.ToString(caller.Account)
	if actualAccountID != account.AccountID {
		return nil, fmt.Errorf("account ID mismatch: registered %s, but assumed credentials belong to %s",
			account.AccountID, actualAccountID)
	}

	regions, err := p.DiscoverRegions(ctx, account)
	if err != nil {
		// Non-fatal for verification: fallback to configured account regions
		regions = account.Regions
	}

	return &provider.ConnectionVerificationResult{
		Connected:         true,
		AccountID:         actualAccountID,
		CallerARN:         aws.ToString(caller.Arn),
		DiscoveredRegions: regions,
		Details: map[string]interface{}{
			"user_id": aws.ToString(caller.UserId),
		},
	}, nil
}

// DiscoverRegions returns all enabled geographic AWS regions for the account.
func (p *AWSProvider) DiscoverRegions(ctx context.Context, account *domain.CloudAccount) ([]string, error) {
	clientSet, err := p.factory.AssumeRoleClientSet(ctx, account, "us-east-1")
	if err != nil {
		return nil, err
	}

	out, err := clientSet.EC2.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false), // Only opted-in/enabled regions
	})
	if err != nil {
		return nil, fmt.Errorf("failed DescribeRegions: %w", err)
	}

	var regions []string
	for _, r := range out.Regions {
		if r.RegionName != nil {
			regions = append(regions, *r.RegionName)
		}
	}
	return regions, nil
}

// DiscoverResources coordinates multi-region, multi-service read-only infrastructure discovery.
func (p *AWSProvider) DiscoverResources(ctx context.Context, req provider.DiscoveryRequest) (*provider.DiscoveryResult, error) {
	startTime := time.Now().UTC()
	result := &provider.DiscoveryResult{
		JobID:             req.JobID,
		AccountID:         req.Account.AccountID,
		DiscoveredRegions: req.Regions,
		StartedAt:         startTime,
	}

	// S3 is a global control plane service; discover once
	s3Discovered := false

	for _, region := range req.Regions {
		if err := p.factory.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		clientSet, err := p.factory.AssumeRoleClientSet(ctx, &req.Account, region)
		if err != nil {
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    region,
				Service:   "auth",
				Code:      "AssumeRoleFailed",
				Message:   err.Error(),
				Retryable: false,
			})
			continue // Proceed to next region (partial failure resilience)
		}

		// 1. EC2 & Block Storage
		ec2Entities, ec2Rels, err := adapters.DiscoverEC2(ctx, clientSet.EC2, &req.Account, region)
		if err != nil {
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    region,
				Service:   "ec2",
				Code:      "EC2DiscoveryFailed",
				Message:   err.Error(),
				Retryable: true,
			})
		} else {
			result.Entities = append(result.Entities, ec2Entities...)
			result.Relationships = append(result.Relationships, ec2Rels...)
		}

		// 2. VPC & Networking
		vpcEntities, vpcRels, err := adapters.DiscoverVPC(ctx, clientSet.EC2, &req.Account, region)
		if err != nil {
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    region,
				Service:   "vpc",
				Code:      "VPCDiscoveryFailed",
				Message:   err.Error(),
				Retryable: true,
			})
		} else {
			result.Entities = append(result.Entities, vpcEntities...)
			result.Relationships = append(result.Relationships, vpcRels...)
		}

		// 3. S3 Storage (Global)
		if !s3Discovered {
			s3Entities, s3Rels, err := adapters.DiscoverS3(ctx, clientSet.S3, &req.Account, region)
			if err != nil {
				result.Errors = append(result.Errors, provider.ServiceError{
					Region:    region,
					Service:   "s3",
					Code:      "S3DiscoveryFailed",
					Message:   err.Error(),
					Retryable: true,
				})
			} else {
				result.Entities = append(result.Entities, s3Entities...)
				result.Relationships = append(result.Relationships, s3Rels...)
				s3Discovered = true
			}
		}

		// 4. RDS Databases
		rdsEntities, rdsRels, err := adapters.DiscoverRDS(ctx, clientSet.RDS, &req.Account, region)
		if err != nil {
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    region,
				Service:   "rds",
				Code:      "RDSDiscoveryFailed",
				Message:   err.Error(),
				Retryable: true,
			})
		} else {
			result.Entities = append(result.Entities, rdsEntities...)
			result.Relationships = append(result.Relationships, rdsRels...)
		}

		// 5. Elastic Load Balancing v2
		elbEntities, elbRels, err := adapters.DiscoverELBv2(ctx, clientSet.ELB, &req.Account, region)
		if err != nil {
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    region,
				Service:   "elbv2",
				Code:      "ELBDiscoveryFailed",
				Message:   err.Error(),
				Retryable: true,
			})
		} else {
			result.Entities = append(result.Entities, elbEntities...)
			result.Relationships = append(result.Relationships, elbRels...)
		}

		// 6. Lambda Serverless
		lambdaEntities, lambdaRels, err := adapters.DiscoverLambda(ctx, clientSet.Lambda, &req.Account, region)
		if err != nil {
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    region,
				Service:   "lambda",
				Code:      "LambdaDiscoveryFailed",
				Message:   err.Error(),
				Retryable: true,
			})
		} else {
			result.Entities = append(result.Entities, lambdaEntities...)
			result.Relationships = append(result.Relationships, lambdaRels...)
		}

		// 7. ECS & EKS Containers
		contEntities, contRels, err := adapters.DiscoverContainers(ctx, clientSet.ECS, clientSet.EKS, &req.Account, region)
		if err != nil {
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    region,
				Service:   "containers",
				Code:      "ContainersDiscoveryFailed",
				Message:   err.Error(),
				Retryable: true,
			})
		} else {
			result.Entities = append(result.Entities, contEntities...)
			result.Relationships = append(result.Relationships, contRels...)
		}
	}

	// 8. Cost Explorer spend & usage data (Global endpoint in us-east-1)
	if clientSetUS, err := p.factory.AssumeRoleClientSet(ctx, &req.Account, "us-east-1"); err == nil && clientSetUS.CostExplorer != nil {
		now := time.Now().UTC()
		startDate := now.AddDate(0, 0, -30).Format("2006-01-02")
		endDate := now.Format("2006-01-02")
		costSnapshots, err := adapters.FetchCostAndUsage(ctx, clientSetUS.CostExplorer, &req.Account, startDate, endDate)
		if err != nil {
			// Record partial error - non-fatal, does not block resource discovery
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    "global",
				Service:   "costexplorer",
				Code:      "CostExplorerAccessDenied",
				Message:   err.Error(),
				Retryable: false,
			})
		} else {
			result.CostSnapshots = append(result.CostSnapshots, costSnapshots...)
		}
	}

	result.CompletedAt = time.Now().UTC()
	return result, nil
}
