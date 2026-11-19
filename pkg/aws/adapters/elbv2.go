package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// DiscoverELBv2 catalogs Application and Network Load Balancers, Target Groups, and Target health,
// and synthesizes relationships to VPCs, Subnets, Security Groups, and Target EC2 instances.
func DiscoverELBv2(ctx context.Context, client *elasticloadbalancingv2.Client, account *domain.CloudAccount, region string) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	var entities []domain.ResourceEntity
	var relationships []domain.ResourceRelationship
	now := time.Now().UTC()

	lbArnToID := make(map[string]uuid.UUID)

	// -------------------------------------------------------------
	// 1. Load Balancers (Paginated)
	// -------------------------------------------------------------
	lbPaginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for lbPaginator.HasMorePages() {
		page, err := lbPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeLoadBalancers in %s: %w", region, err)
		}

		for _, lb := range page.LoadBalancers {
			lbARN := aws.ToString(lb.LoadBalancerArn)
			lbName := aws.ToString(lb.LoadBalancerName)

			var createTime *time.Time
			if lb.CreatedTime != nil {
				t := *lb.CreatedTime
				createTime = &t
			}

			status := domain.ResourceStatusAvailable
			if lb.State != nil && string(lb.State.Code) == "failed" {
				status = domain.ResourceStatusDegraded
			}

			observed := map[string]interface{}{
				"load_balancer_name": lbName,
				"dns_name":           aws.ToString(lb.DNSName),
				"type":               string(lb.Type),
				"scheme":             string(lb.Scheme),
				"vpc_id":             aws.ToString(lb.VpcId),
				"ip_address_type":    string(lb.IpAddressType),
				"state":              lb.State,
			}

			entity := domain.ResourceEntity{
				ID:                uuid.New(),
				TenantID:          account.TenantID,
				CloudAccountID:    account.ID,
				Provider:          domain.ProviderAWS,
				AccountID:         account.AccountID,
				Region:            region,
				ResourceType:      "aws:elbv2:load-balancer",
				NativeID:          lbName,
				ARN:               lbARN,
				Name:              lbName,
				Status:            status,
				Tags:              make(map[string]string),
				CreatedAtProvider: createTime,
				ObservedAt:        now,
				ObservedState:     observed,
				DerivedState: map[string]interface{}{
					"is_internet_facing": string(lb.Scheme) == "internet-facing",
				},
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
			lbArnToID[lbARN] = entity.ID

			// Relationship: LB -> VPC
			if vpcID := aws.ToString(lb.VpcId); vpcID != "" {
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

			// Relationship: LB -> Subnets
			for _, az := range lb.AvailabilityZones {
				if subID := aws.ToString(az.SubnetId); subID != "" {
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

			// Relationship: LB -> Security Groups
			for _, sgID := range lb.SecurityGroups {
				if sgID != "" {
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
		}
	}

	// -------------------------------------------------------------
	// 2. Target Groups & Target Health
	// -------------------------------------------------------------
	tgPaginator := elasticloadbalancingv2.NewDescribeTargetGroupsPaginator(client, &elasticloadbalancingv2.DescribeTargetGroupsInput{})
	for tgPaginator.HasMorePages() {
		page, err := tgPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeTargetGroups in %s: %w", region, err)
		}

		for _, tg := range page.TargetGroups {
			tgARN := aws.ToString(tg.TargetGroupArn)
			tgName := aws.ToString(tg.TargetGroupName)

			observed := map[string]interface{}{
				"target_group_name": tgName,
				"target_type":       string(tg.TargetType),
				"protocol":          string(tg.Protocol),
				"port":              aws.ToInt32(tg.Port),
				"vpc_id":            aws.ToString(tg.VpcId),
			}

			tgEntity := domain.ResourceEntity{
				ID:             uuid.New(),
				TenantID:       account.TenantID,
				CloudAccountID: account.ID,
				Provider:       domain.ProviderAWS,
				AccountID:      account.AccountID,
				Region:         region,
				ResourceType:   "aws:elbv2:target-group",
				NativeID:       tgName,
				ARN:            tgARN,
				Name:           tgName,
				Status:         domain.ResourceStatusAvailable,
				Tags:           make(map[string]string),
				ObservedAt:     now,
				ObservedState:  observed,
			}
			_ = tgEntity.ComputeConfigurationHash()
			entities = append(entities, tgEntity)

			// Relationship: TargetGroup -> VPC
			if vpcID := aws.ToString(tg.VpcId); vpcID != "" {
				relationships = append(relationships, domain.ResourceRelationship{
					ID:               uuid.New(),
					TenantID:         account.TenantID,
					SourceResourceID: tgEntity.ID,
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

			// Relationship: LB -> ROUTES_TO -> TargetGroup
			for _, lbARN := range tg.LoadBalancerArns {
				if lbID, found := lbArnToID[lbARN]; found {
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: lbID,
						TargetResourceID: tgEntity.ID,
						RelationshipType: domain.RelRoutesTo,
						Confidence:       1.0,
						Provenance:       domain.ProvenanceObserved,
						Properties: map[string]interface{}{
							"target_arn": tgARN,
						},
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				} else {
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: tgEntity.ID,
						TargetARN:        lbARN,
						RelationshipType: domain.RelAttachedTo,
						Confidence:       1.0,
						Provenance:       domain.ProvenanceObserved,
						Properties: map[string]interface{}{
							"target_arn": lbARN,
						},
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				}
			}

			// Discover Targets via DescribeTargetHealth
			healthResp, err := client.DescribeTargetHealth(ctx, &elasticloadbalancingv2.DescribeTargetHealthInput{
				TargetGroupArn: tg.TargetGroupArn,
			})
			if err == nil && healthResp != nil {
				for _, thd := range healthResp.TargetHealthDescriptions {
					if thd.Target != nil && thd.Target.Id != nil {
						targetID := aws.ToString(thd.Target.Id)
						if targetID != "" {
							healthState := ""
							if thd.TargetHealth != nil {
								healthState = string(thd.TargetHealth.State)
							}
							relationships = append(relationships, domain.ResourceRelationship{
								ID:               uuid.New(),
								TenantID:         account.TenantID,
								SourceResourceID: tgEntity.ID,
								TargetNativeID:   targetID,
								RelationshipType: domain.RelTargets,
								Confidence:       1.0,
								Provenance:       domain.ProvenanceObserved,
								Properties: map[string]interface{}{
									"target_native_id": targetID,
									"port":             aws.ToInt32(thd.Target.Port),
									"health_state":     healthState,
								},
								DiscoveredAt: now,
								UpdatedAt:    now,
							})
						}
					}
				}
			}
		}
	}

	return entities, relationships, nil
}
