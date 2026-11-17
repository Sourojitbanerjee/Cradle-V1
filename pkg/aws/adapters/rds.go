package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// DiscoverRDS catalogs RDS database instances and Aurora clusters.
func DiscoverRDS(ctx context.Context, client *rds.Client, account *domain.CloudAccount, region string) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	var entities []domain.ResourceEntity
	var relationships []domain.ResourceRelationship
	now := time.Now().UTC()

	// -------------------------------------------------------------
	// 1. RDS DB Instances
	// -------------------------------------------------------------
	instPaginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for instPaginator.HasMorePages() {
		page, err := instPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeDBInstances in %s: %w", region, err)
		}

		for _, db := range page.DBInstances {
			dbID := aws.ToString(db.DBInstanceIdentifier)
			arn := aws.ToString(db.DBInstanceArn)
			if arn == "" {
				arn = fmt.Sprintf("arn:aws:rds:%s:%s:db:%s", region, account.AccountID, dbID)
			}

			tags := make(map[string]string)
			for _, t := range db.TagList {
				if t.Key != nil && t.Value != nil {
					tags[*t.Key] = *t.Value
				}
			}

			status := domain.ResourceStatusAvailable
			if aws.ToString(db.DBInstanceStatus) == "stopped" {
				status = domain.ResourceStatusStopped
			} else if aws.ToString(db.DBInstanceStatus) == "deleting" {
				status = domain.ResourceStatusTerminated
			}

			var createTime *time.Time
			if db.InstanceCreateTime != nil {
				t := *db.InstanceCreateTime
				createTime = &t
			}

			observed := map[string]interface{}{
				"db_instance_id":      dbID,
				"engine":              aws.ToString(db.Engine),
				"engine_version":      aws.ToString(db.EngineVersion),
				"instance_class":      aws.ToString(db.DBInstanceClass),
				"allocated_storage":   aws.ToInt32(db.AllocatedStorage),
				"storage_type":        aws.ToString(db.StorageType),
				"storage_encrypted":   aws.ToBool(db.StorageEncrypted),
				"kms_key_id":          aws.ToString(db.KmsKeyId),
				"multi_az":            aws.ToBool(db.MultiAZ),
				"publicly_accessible": aws.ToBool(db.PubliclyAccessible),
				"status":              aws.ToString(db.DBInstanceStatus),
				"vpc_id":              "",
			}

			if db.DBSubnetGroup != nil {
				observed["vpc_id"] = aws.ToString(db.DBSubnetGroup.VpcId)
			}

			entity := domain.ResourceEntity{
				ID:                uuid.New(),
				TenantID:          account.TenantID,
				CloudAccountID:    account.ID,
				Provider:          domain.ProviderAWS,
				AccountID:         account.AccountID,
				Region:            region,
				ResourceType:      "aws:rds:db-instance",
				NativeID:          dbID,
				ARN:               arn,
				Name:              dbID,
				Status:            status,
				Tags:              tags,
				CreatedAtProvider: createTime,
				ObservedAt:        now,
				ObservedState:     observed,
				DerivedState: map[string]interface{}{
					"is_encrypted": aws.ToBool(db.StorageEncrypted),
					"is_multi_az":  aws.ToBool(db.MultiAZ),
				},
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
		}
	}

	// -------------------------------------------------------------
	// 2. RDS DB Clusters (Aurora)
	// -------------------------------------------------------------
	clusterPaginator := rds.NewDescribeDBClustersPaginator(client, &rds.DescribeDBClustersInput{})
	for clusterPaginator.HasMorePages() {
		page, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeDBClusters in %s: %w", region, err)
		}

		for _, cl := range page.DBClusters {
			clID := aws.ToString(cl.DBClusterIdentifier)
			arn := aws.ToString(cl.DBClusterArn)
			if arn == "" {
				arn = fmt.Sprintf("arn:aws:rds:%s:%s:cluster:%s", region, account.AccountID, clID)
			}

			tags := make(map[string]string)
			for _, t := range cl.TagList {
				if t.Key != nil && t.Value != nil {
					tags[*t.Key] = *t.Value
				}
			}

			var createTime *time.Time
			if cl.ClusterCreateTime != nil {
				t := *cl.ClusterCreateTime
				createTime = &t
			}

			observed := map[string]interface{}{
				"db_cluster_id":     clID,
				"engine":            aws.ToString(cl.Engine),
				"engine_version":    aws.ToString(cl.EngineVersion),
				"status":            aws.ToString(cl.Status),
				"storage_encrypted": aws.ToBool(cl.StorageEncrypted),
				"multi_az":          aws.ToBool(cl.MultiAZ),
				"members_count":     len(cl.DBClusterMembers),
			}

			entity := domain.ResourceEntity{
				ID:                uuid.New(),
				TenantID:          account.TenantID,
				CloudAccountID:    account.ID,
				Provider:          domain.ProviderAWS,
				AccountID:         account.AccountID,
				Region:            region,
				ResourceType:      "aws:rds:db-cluster",
				NativeID:          clID,
				ARN:               arn,
				Name:              clID,
				Status:            domain.ResourceStatusAvailable,
				Tags:              tags,
				CreatedAtProvider: createTime,
				ObservedAt:        now,
				ObservedState:     observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
		}
	}

	return entities, relationships, nil
}
