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

// DiscoverRDS catalogs RDS database instances and Aurora clusters, and synthesizes
// relationships to VPCs, Subnets, Security Groups, and KMS keys.
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

			// Relationships: RDS -> VPC & Subnets
			if db.DBSubnetGroup != nil {
				if vpcID := aws.ToString(db.DBSubnetGroup.VpcId); vpcID != "" {
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: entity.ID,
						TargetNativeID:   vpcID,
						RelationshipType: domain.RelAttachedTo,
						Confidence:       1.0,
						Provenance:       domain.ProvenanceObserved,
						Properties: map[string]interface{}{
							"target_native_id": vpcID,
							"target_type":      "aws:ec2:vpc",
						},
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				}
				for _, sub := range db.DBSubnetGroup.Subnets {
					if subID := aws.ToString(sub.SubnetIdentifier); subID != "" {
						relationships = append(relationships, domain.ResourceRelationship{
							ID:               uuid.New(),
							TenantID:         account.TenantID,
							SourceResourceID: entity.ID,
							TargetNativeID:   subID,
							RelationshipType: domain.RelAttachedTo,
							Confidence:       1.0,
							Provenance:       domain.ProvenanceObserved,
							Properties: map[string]interface{}{
								"target_native_id": subID,
								"target_type":      "aws:ec2:subnet",
							},
							DiscoveredAt: now,
							UpdatedAt:    now,
						})
					}
				}
			}

			// Relationships: RDS -> Security Groups
			for _, sg := range db.VpcSecurityGroups {
				if sgID := aws.ToString(sg.VpcSecurityGroupId); sgID != "" {
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: entity.ID,
						TargetNativeID:   sgID,
						RelationshipType: domain.RelAttachedTo,
						Confidence:       1.0,
						Provenance:       domain.ProvenanceObserved,
						Properties: map[string]interface{}{
							"target_native_id": sgID,
							"target_type":      "aws:ec2:security-group",
						},
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				}
			}

			// Relationship: RDS -> KMS Key
			if kmsKey := aws.ToString(db.KmsKeyId); kmsKey != "" {
				relationships = append(relationships, domain.ResourceRelationship{
					ID:               uuid.New(),
					TenantID:         account.TenantID,
					SourceResourceID: entity.ID,
					TargetARN:        kmsKey,
					TargetNativeID:   kmsKey,
					RelationshipType: domain.RelEncryptsWith,
					Confidence:       1.0,
					Provenance:       domain.ProvenanceObserved,
					Properties: map[string]interface{}{
						"target_arn":  kmsKey,
						"target_type": "aws:kms:key",
					},
					DiscoveredAt: now,
					UpdatedAt:    now,
				})
			}
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

			// Relationships: Cluster -> Security Groups
			for _, sg := range cl.VpcSecurityGroups {
				if sgID := aws.ToString(sg.VpcSecurityGroupId); sgID != "" {
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: entity.ID,
						TargetNativeID:   sgID,
						RelationshipType: domain.RelAttachedTo,
						Confidence:       1.0,
						Provenance:       domain.ProvenanceObserved,
						Properties: map[string]interface{}{
							"target_native_id": sgID,
							"target_type":      "aws:ec2:security-group",
						},
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				}
			}

			// Relationship: Cluster -> KMS Key
			if kmsKey := aws.ToString(cl.KmsKeyId); kmsKey != "" {
				relationships = append(relationships, domain.ResourceRelationship{
					ID:               uuid.New(),
					TenantID:         account.TenantID,
					SourceResourceID: entity.ID,
					TargetARN:        kmsKey,
					TargetNativeID:   kmsKey,
					RelationshipType: domain.RelEncryptsWith,
					Confidence:       1.0,
					Provenance:       domain.ProvenanceObserved,
					Properties: map[string]interface{}{
						"target_arn":  kmsKey,
						"target_type": "aws:kms:key",
					},
					DiscoveredAt: now,
					UpdatedAt:    now,
				})
			}
		}
	}

	return entities, relationships, nil
}
