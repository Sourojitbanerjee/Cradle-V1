package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// DiscoverContainers catalogs ECS clusters/services and EKS managed Kubernetes clusters/nodegroups,
// and synthesizes relationships to VPCs, Subnets, and managed EC2 instances.
func DiscoverContainers(
	ctx context.Context,
	ecsClient *ecs.Client,
	eksClient *eks.Client,
	account *domain.CloudAccount,
	region string,
) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	var entities []domain.ResourceEntity
	var relationships []domain.ResourceRelationship
	now := time.Now().UTC()

	// -------------------------------------------------------------
	// 1. ECS Clusters & Services
	// -------------------------------------------------------------
	clustersOutput, err := ecsClient.ListClusters(ctx, &ecs.ListClustersInput{})
	if err == nil && clustersOutput != nil && len(clustersOutput.ClusterArns) > 0 {
		descOutput, err := ecsClient.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: clustersOutput.ClusterArns,
		})
		if err == nil && descOutput != nil {
			for _, cl := range descOutput.Clusters {
				clName := aws.ToString(cl.ClusterName)
				arn := aws.ToString(cl.ClusterArn)

				observed := map[string]interface{}{
					"cluster_name":                   clName,
					"status":                         aws.ToString(cl.Status),
					"running_tasks_count":            cl.RunningTasksCount,
					"pending_tasks_count":            cl.PendingTasksCount,
					"active_services_count":          cl.ActiveServicesCount,
					"registered_container_instances": cl.RegisteredContainerInstancesCount,
				}

				entity := domain.ResourceEntity{
					ID:             uuid.New(),
					TenantID:       account.TenantID,
					CloudAccountID: account.ID,
					Provider:       domain.ProviderAWS,
					AccountID:      account.AccountID,
					Region:         region,
					ResourceType:   "aws:ecs:cluster",
					NativeID:       clName,
					ARN:            arn,
					Name:           clName,
					Status:         domain.ResourceStatusAvailable,
					Tags:           make(map[string]string),
					ObservedAt:     now,
					ObservedState:  observed,
				}
				_ = entity.ComputeConfigurationHash()
				entities = append(entities, entity)
			}
		}
	}

	// -------------------------------------------------------------
	// 2. EKS Managed Kubernetes Clusters & NodeGroups
	// -------------------------------------------------------------
	eksClusters, err := eksClient.ListClusters(ctx, &eks.ListClustersInput{})
	if err == nil && eksClusters != nil {
		for _, clusterName := range eksClusters.Clusters {
			desc, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
				Name: aws.String(clusterName),
			})
			if err != nil || desc == nil || desc.Cluster == nil {
				continue
			}

			cl := desc.Cluster
			arn := aws.ToString(cl.Arn)

			var createTime *time.Time
			if cl.CreatedAt != nil {
				t := *cl.CreatedAt
				createTime = &t
			}

			observed := map[string]interface{}{
				"cluster_name": clusterName,
				"version":      aws.ToString(cl.Version),
				"status":       string(cl.Status),
				"endpoint":     aws.ToString(cl.Endpoint),
				"role_arn":     aws.ToString(cl.RoleArn),
			}

			if cl.ResourcesVpcConfig != nil {
				observed["vpc_id"] = aws.ToString(cl.ResourcesVpcConfig.VpcId)
				observed["subnet_ids"] = cl.ResourcesVpcConfig.SubnetIds
				observed["endpoint_public_access"] = cl.ResourcesVpcConfig.EndpointPublicAccess
			}

			entity := domain.ResourceEntity{
				ID:                uuid.New(),
				TenantID:          account.TenantID,
				CloudAccountID:    account.ID,
				Provider:          domain.ProviderAWS,
				AccountID:         account.AccountID,
				Region:            region,
				ResourceType:      "aws:eks:cluster",
				NativeID:          clusterName,
				ARN:               arn,
				Name:              clusterName,
				Status:            domain.ResourceStatusAvailable,
				Tags:              cl.Tags,
				CreatedAtProvider: createTime,
				ObservedAt:        now,
				ObservedState:     observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)

			// Relationship: EKS Cluster -> VPC
			if cl.ResourcesVpcConfig != nil {
				if vpcID := aws.ToString(cl.ResourcesVpcConfig.VpcId); vpcID != "" {
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
				// Relationship: EKS Cluster -> Subnets
				for _, subID := range cl.ResourcesVpcConfig.SubnetIds {
					if subID != "" {
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

			// Discover Managed NodeGroups for this cluster
			ngList, err := eksClient.ListNodegroups(ctx, &eks.ListNodegroupsInput{
				ClusterName: aws.String(clusterName),
			})
			if err == nil && ngList != nil {
				for _, ngName := range ngList.Nodegroups {
					ngDesc, err := eksClient.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
						ClusterName:   aws.String(clusterName),
						NodegroupName: aws.String(ngName),
					})
					if err != nil || ngDesc == nil || ngDesc.Nodegroup == nil {
						continue
					}

					ng := ngDesc.Nodegroup
					ngARN := aws.ToString(ng.NodegroupArn)

					ngObserved := map[string]interface{}{
						"nodegroup_name": ngName,
						"cluster_name":   clusterName,
						"status":         string(ng.Status),
						"instance_types": ng.InstanceTypes,
						"scaling_config": ng.ScalingConfig,
						"subnets":        ng.Subnets,
					}

					ngEntity := domain.ResourceEntity{
						ID:             uuid.New(),
						TenantID:       account.TenantID,
						CloudAccountID: account.ID,
						Provider:       domain.ProviderAWS,
						AccountID:      account.AccountID,
						Region:         region,
						ResourceType:   "aws:eks:nodegroup",
						NativeID:       fmt.Sprintf("%s/%s", clusterName, ngName),
						ARN:            ngARN,
						Name:           ngName,
						Status:         domain.ResourceStatusAvailable,
						Tags:           ng.Tags,
						ObservedAt:     now,
						ObservedState:  ngObserved,
					}
					_ = ngEntity.ComputeConfigurationHash()
					entities = append(entities, ngEntity)

					// Relationship: Cluster CONTAINS NodeGroup
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: entity.ID,
						TargetResourceID: ngEntity.ID,
						RelationshipType: domain.RelContains,
						Confidence:       1.0,
						Provenance:       domain.ProvenanceObserved,
						DiscoveredAt:     now,
						UpdatedAt:        now,
					})

					// Relationship: NodeGroup -> Subnets
					for _, subID := range ng.Subnets {
						if subID != "" {
							relationships = append(relationships, domain.ResourceRelationship{
								ID:               uuid.New(),
								TenantID:         account.TenantID,
								SourceResourceID: ngEntity.ID,
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

					// Relationship: NodeGroup MANAGES EC2 Instances (Tag match)
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: ngEntity.ID,
						RelationshipType: domain.RelManages,
						Confidence:       0.95,
						Provenance:       domain.ProvenanceObservedTag,
						Properties: map[string]interface{}{
							"match_tag_key": "eks:nodegroup-name",
							"match_tag_val": ngName,
							"cluster_name":  clusterName,
						},
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				}
			}
		}
	}

	return entities, relationships, nil
}
