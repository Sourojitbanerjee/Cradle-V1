package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// DiscoverS3 catalogs S3 buckets and their security/compliance configurations.
func DiscoverS3(ctx context.Context, client *s3.Client, account *domain.CloudAccount, defaultRegion string) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	var entities []domain.ResourceEntity
	now := time.Now().UTC()

	bucketsOutput, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed ListBuckets: %w", err)
	}

	for _, b := range bucketsOutput.Buckets {
		bucketName := aws.ToString(b.Name)
		arn := fmt.Sprintf("arn:aws:s3:::%s", bucketName)

		// Discover bucket true region
		region := defaultRegion
		locOutput, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
			Bucket: aws.String(bucketName),
		})
		if err == nil && locOutput != nil && locOutput.LocationConstraint != "" {
			region = string(locOutput.LocationConstraint)
			if region == "EU" {
				region = "eu-west-1"
			}
		}

		// Retrieve tags safely
		tags := make(map[string]string)
		tagOutput, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
			Bucket: aws.String(bucketName),
		})
		if err == nil && tagOutput != nil {
			for _, t := range tagOutput.TagSet {
				if t.Key != nil && t.Value != nil {
					tags[*t.Key] = *t.Value
				}
			}
		}

		// Retrieve Encryption metadata
		encrypted := false
		encOutput, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
			Bucket: aws.String(bucketName),
		})
		if err == nil && encOutput != nil && encOutput.ServerSideEncryptionConfiguration != nil {
			encrypted = len(encOutput.ServerSideEncryptionConfiguration.Rules) > 0
		}

		// Retrieve Public Access Block settings
		publicBlockActive := false
		pabOutput, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{
			Bucket: aws.String(bucketName),
		})
		if err == nil && pabOutput != nil && pabOutput.PublicAccessBlockConfiguration != nil {
			cfg := pabOutput.PublicAccessBlockConfiguration
			publicBlockActive = aws.ToBool(cfg.BlockPublicAcls) &&
				aws.ToBool(cfg.BlockPublicPolicy) &&
				aws.ToBool(cfg.IgnorePublicAcls) &&
				aws.ToBool(cfg.RestrictPublicBuckets)
		}

		var createTime *time.Time
		if b.CreationDate != nil {
			t := *b.CreationDate
			createTime = &t
		}

		observed := map[string]interface{}{
			"bucket_name":         bucketName,
			"creation_date":       b.CreationDate,
			"encrypted":           encrypted,
			"public_access_block": publicBlockActive,
		}

		entity := domain.ResourceEntity{
			ID:                uuid.New(),
			TenantID:          account.TenantID,
			CloudAccountID:    account.ID,
			Provider:          domain.ProviderAWS,
			AccountID:         account.AccountID,
			Region:            region,
			ResourceType:      "aws:s3:bucket",
			NativeID:          bucketName,
			ARN:               arn,
			Name:              bucketName,
			Status:            domain.ResourceStatusAvailable,
			Tags:              tags,
			CreatedAtProvider: createTime,
			ObservedAt:        now,
			ObservedState:     observed,
			DerivedState: map[string]interface{}{
				"is_encrypted":          encrypted,
				"is_public_block_tight": publicBlockActive,
			},
		}
		_ = entity.ComputeConfigurationHash()
		entities = append(entities, entity)
	}

	return entities, nil, nil
}
