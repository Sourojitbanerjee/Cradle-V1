package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// DiscoverVPC discovers VPCs, Subnets, Route Tables, Security Groups, and NAT Gateways.
func DiscoverVPC(ctx context.Context, client *ec2.Client, account *domain.CloudAccount, region string) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	var entities []domain.ResourceEntity
	var relationships []domain.ResourceRelationship
	now := time.Now().UTC()

	subnetEntityMap := make(map[string]uuid.UUID)

	// -------------------------------------------------------------
	// 1. Virtual Private Clouds (VPCs)
	// -------------------------------------------------------------
	vpcPaginator := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})
	for vpcPaginator.HasMorePages() {
		page, err := vpcPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeVpcs in %s: %w", region, err)
		}

		for _, v := range page.Vpcs {
			vpcID := aws.ToString(v.VpcId)
			tags := parseEC2Tags(v.Tags)
			name := tags["Name"]
			if name == "" {
				name = vpcID
			}

			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:vpc/%s", region, account.AccountID, vpcID)
			observed := map[string]interface{}{
				"vpc_id":          vpcID,
				"cidr_block":      aws.ToString(v.CidrBlock),
				"is_default":      aws.ToBool(v.IsDefault),
				"state":           string(v.State),
				"dhcp_options_id": aws.ToString(v.DhcpOptionsId),
			}

			entity := domain.ResourceEntity{
				ID:             uuid.New(),
				TenantID:       account.TenantID,
				CloudAccountID: account.ID,
				Provider:       domain.ProviderAWS,
				AccountID:      account.AccountID,
				Region:         region,
				ResourceType:   "aws:ec2:vpc",
				NativeID:       vpcID,
				ARN:            arn,
				Name:           name,
				Status:         domain.ResourceStatusAvailable,
				Tags:           tags,
				ObservedAt:     now,
				ObservedState:  observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
		}
	}

	// -------------------------------------------------------------
	// 2. Subnets
	// -------------------------------------------------------------
	subnetPaginator := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})
	for subnetPaginator.HasMorePages() {
		page, err := subnetPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeSubnets in %s: %w", region, err)
		}

		for _, sub := range page.Subnets {
			subID := aws.ToString(sub.SubnetId)
			tags := parseEC2Tags(sub.Tags)
			name := tags["Name"]
			if name == "" {
				name = subID
			}

			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:subnet/%s", region, account.AccountID, subID)
			observed := map[string]interface{}{
				"subnet_id":         subID,
				"vpc_id":            aws.ToString(sub.VpcId),
				"cidr_block":        aws.ToString(sub.CidrBlock),
				"availability_zone": aws.ToString(sub.AvailabilityZone),
				"available_ips":     aws.ToInt32(sub.AvailableIpAddressCount),
				"map_public_ip":     aws.ToBool(sub.MapPublicIpOnLaunch),
				"state":             string(sub.State),
			}

			entity := domain.ResourceEntity{
				ID:             uuid.New(),
				TenantID:       account.TenantID,
				CloudAccountID: account.ID,
				Provider:       domain.ProviderAWS,
				AccountID:      account.AccountID,
				Region:         region,
				ResourceType:   "aws:ec2:subnet",
				NativeID:       subID,
				ARN:            arn,
				Name:           name,
				Status:         domain.ResourceStatusAvailable,
				Tags:           tags,
				ObservedAt:     now,
				ObservedState:  observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
			subnetEntityMap[subID] = entity.ID

			// Relate Subnet -> VPC: Subnet is attached to VPC
			if sub.VpcId != nil && *sub.VpcId != "" {
				vpcARN := fmt.Sprintf("arn:aws:ec2:%s:%s:vpc/%s", region, account.AccountID, *sub.VpcId)
				relationships = append(relationships, domain.ResourceRelationship{
					ID:               uuid.New(),
					TenantID:         account.TenantID,
					SourceResourceID: entity.ID,
					TargetARN:        vpcARN,
					TargetNativeID:   *sub.VpcId,
					RelationshipType: domain.RelAttachedTo,
					Properties: map[string]interface{}{
						"parent_vpc_arn": vpcARN,
						"vpc_id":         *sub.VpcId,
					},
					Confidence:   1.0,
					Provenance:   domain.ProvenanceObserved,
					DiscoveredAt: now,
					UpdatedAt:    now,
				})
			}
		}
	}

	// -------------------------------------------------------------
	// 3. Security Groups
	// -------------------------------------------------------------
	sgPaginator := ec2.NewDescribeSecurityGroupsPaginator(client, &ec2.DescribeSecurityGroupsInput{})
	for sgPaginator.HasMorePages() {
		page, err := sgPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeSecurityGroups in %s: %w", region, err)
		}

		for _, sg := range page.SecurityGroups {
			sgID := aws.ToString(sg.GroupId)
			tags := parseEC2Tags(sg.Tags)
			name := aws.ToString(sg.GroupName)

			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:security-group/%s", region, account.AccountID, sgID)
			observed := map[string]interface{}{
				"group_id":             sgID,
				"group_name":           name,
				"description":          aws.ToString(sg.Description),
				"vpc_id":               aws.ToString(sg.VpcId),
				"inbound_rules_count":  len(sg.IpPermissions),
				"outbound_rules_count": len(sg.IpPermissionsEgress),
			}

			entity := domain.ResourceEntity{
				ID:             uuid.New(),
				TenantID:       account.TenantID,
				CloudAccountID: account.ID,
				Provider:       domain.ProviderAWS,
				AccountID:      account.AccountID,
				Region:         region,
				ResourceType:   "aws:ec2:security-group",
				NativeID:       sgID,
				ARN:            arn,
				Name:           name,
				Status:         domain.ResourceStatusAvailable,
				Tags:           tags,
				ObservedAt:     now,
				ObservedState:  observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)

			// Relate Security Group -> VPC
			if sg.VpcId != nil && *sg.VpcId != "" {
				vpcARN := fmt.Sprintf("arn:aws:ec2:%s:%s:vpc/%s", region, account.AccountID, *sg.VpcId)
				relationships = append(relationships, domain.ResourceRelationship{
					ID:               uuid.New(),
					TenantID:         account.TenantID,
					SourceResourceID: entity.ID,
					TargetARN:        vpcARN,
					TargetNativeID:   *sg.VpcId,
					RelationshipType: domain.RelAttachedTo,
					Properties: map[string]interface{}{
						"vpc_id": *sg.VpcId,
					},
					Confidence:   1.0,
					Provenance:   domain.ProvenanceObserved,
					DiscoveredAt: now,
					UpdatedAt:    now,
				})
			}
		}
	}

	// -------------------------------------------------------------
	// 4. Route Tables (Subnet -> Route Table -> NAT Gateway -> VPC)
	// -------------------------------------------------------------
	rtPaginator := ec2.NewDescribeRouteTablesPaginator(client, &ec2.DescribeRouteTablesInput{})
	for rtPaginator.HasMorePages() {
		page, err := rtPaginator.NextPage(ctx)
		if err != nil {
			break // gracefully continue on permissions
		}

		for _, rt := range page.RouteTables {
			rtID := aws.ToString(rt.RouteTableId)
			tags := parseEC2Tags(rt.Tags)
			name := tags["Name"]
			if name == "" {
				name = rtID
			}

			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:route-table/%s", region, account.AccountID, rtID)
			observed := map[string]interface{}{
				"route_table_id": rtID,
				"vpc_id":         aws.ToString(rt.VpcId),
				"routes_count":   len(rt.Routes),
			}

			entity := domain.ResourceEntity{
				ID:             uuid.New(),
				TenantID:       account.TenantID,
				CloudAccountID: account.ID,
				Provider:       domain.ProviderAWS,
				AccountID:      account.AccountID,
				Region:         region,
				ResourceType:   "aws:ec2:route-table",
				NativeID:       rtID,
				ARN:            arn,
				Name:           name,
				Status:         domain.ResourceStatusAvailable,
				Tags:           tags,
				ObservedAt:     now,
				ObservedState:  observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)

			// Relate Route Table -> VPC
			if rt.VpcId != nil && *rt.VpcId != "" {
				vpcARN := fmt.Sprintf("arn:aws:ec2:%s:%s:vpc/%s", region, account.AccountID, *rt.VpcId)
				relationships = append(relationships, domain.ResourceRelationship{
					ID:               uuid.New(),
					TenantID:         account.TenantID,
					SourceResourceID: entity.ID,
					TargetARN:        vpcARN,
					TargetNativeID:   *rt.VpcId,
					RelationshipType: domain.RelAttachedTo,
					Properties: map[string]interface{}{
						"vpc_id": *rt.VpcId,
					},
					Confidence:   1.0,
					Provenance:   domain.ProvenanceObserved,
					DiscoveredAt: now,
					UpdatedAt:    now,
				})
			}

			// Relate Subnet -> ROUTES_TO -> Route Table
			for _, assoc := range rt.Associations {
				if assoc.SubnetId != nil && *assoc.SubnetId != "" {
					subID := *assoc.SubnetId
					subnetSourceID := subnetEntityMap[subID]
					subnetARN := fmt.Sprintf("arn:aws:ec2:%s:%s:subnet/%s", region, account.AccountID, subID)

					rel := domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						TargetResourceID: entity.ID,
						RelationshipType: domain.RelRoutesTo,
						Properties: map[string]interface{}{
							"subnet_id":      subID,
							"route_table_id": rtID,
							"is_main":        aws.ToBool(assoc.Main),
						},
						Confidence:   1.0,
						Provenance:   domain.ProvenanceObserved,
						DiscoveredAt: now,
						UpdatedAt:    now,
					}
					if subnetSourceID != uuid.Nil {
						rel.SourceResourceID = subnetSourceID
					} else {
						rel.Properties["source_native_id"] = subID
						rel.Properties["source_arn"] = subnetARN
					}
					if rel.SourceResourceID != uuid.Nil {
						relationships = append(relationships, rel)
					}
				}
			}

			// Relate Route Table -> ROUTES_TO -> NAT Gateway
			for _, r := range rt.Routes {
				if r.NatGatewayId != nil && *r.NatGatewayId != "" {
					natID := *r.NatGatewayId
					natARN := fmt.Sprintf("arn:aws:ec2:%s:%s:natgateway/%s", region, account.AccountID, natID)
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: entity.ID,
						TargetARN:        natARN,
						TargetNativeID:   natID,
						RelationshipType: domain.RelRoutesTo,
						Properties: map[string]interface{}{
							"destination_cidr": aws.ToString(r.DestinationCidrBlock),
							"nat_gateway_id":   natID,
						},
						Confidence:   1.0,
						Provenance:   domain.ProvenanceObserved,
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				}
			}
		}
	}

	// -------------------------------------------------------------
	// 5. NAT Gateways
	// -------------------------------------------------------------
	natPaginator := ec2.NewDescribeNatGatewaysPaginator(client, &ec2.DescribeNatGatewaysInput{})
	for natPaginator.HasMorePages() {
		page, err := natPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeNatGateways in %s: %w", region, err)
		}

		for _, nat := range page.NatGateways {
			natID := aws.ToString(nat.NatGatewayId)
			tags := parseEC2Tags(nat.Tags)
			name := tags["Name"]
			if name == "" {
				name = natID
			}

			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:natgateway/%s", region, account.AccountID, natID)
			status := domain.ResourceStatusAvailable
			if string(nat.State) == "deleted" || string(nat.State) == "deleting" {
				status = domain.ResourceStatusTerminated
			}

			var createTime *time.Time
			if nat.CreateTime != nil {
				t := *nat.CreateTime
				createTime = &t
			}

			observed := map[string]interface{}{
				"nat_gateway_id": natID,
				"vpc_id":         aws.ToString(nat.VpcId),
				"subnet_id":      aws.ToString(nat.SubnetId),
				"state":          string(nat.State),
				"addresses":      nat.NatGatewayAddresses,
			}

			entity := domain.ResourceEntity{
				ID:                uuid.New(),
				TenantID:          account.TenantID,
				CloudAccountID:    account.ID,
				Provider:          domain.ProviderAWS,
				AccountID:         account.AccountID,
				Region:            region,
				ResourceType:      "aws:ec2:nat-gateway",
				NativeID:          natID,
				ARN:               arn,
				Name:              name,
				Status:            status,
				Tags:              tags,
				CreatedAtProvider: createTime,
				ObservedAt:        now,
				ObservedState:     observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)

			// Relate NAT Gateway -> Subnet
			if nat.SubnetId != nil && *nat.SubnetId != "" {
				subnetARN := fmt.Sprintf("arn:aws:ec2:%s:%s:subnet/%s", region, account.AccountID, *nat.SubnetId)
				relationships = append(relationships, domain.ResourceRelationship{
					ID:               uuid.New(),
					TenantID:         account.TenantID,
					SourceResourceID: entity.ID,
					TargetARN:        subnetARN,
					TargetNativeID:   *nat.SubnetId,
					RelationshipType: domain.RelAttachedTo,
					Properties: map[string]interface{}{
						"subnet_id": *nat.SubnetId,
					},
					Confidence:   1.0,
					Provenance:   domain.ProvenanceObserved,
					DiscoveredAt: now,
					UpdatedAt:    now,
				})
			}

			// Relate NAT Gateway -> VPC
			if nat.VpcId != nil && *nat.VpcId != "" {
				vpcARN := fmt.Sprintf("arn:aws:ec2:%s:%s:vpc/%s", region, account.AccountID, *nat.VpcId)
				relationships = append(relationships, domain.ResourceRelationship{
					ID:               uuid.New(),
					TenantID:         account.TenantID,
					SourceResourceID: entity.ID,
					TargetARN:        vpcARN,
					TargetNativeID:   *nat.VpcId,
					RelationshipType: domain.RelAttachedTo,
					Properties: map[string]interface{}{
						"vpc_id": *nat.VpcId,
					},
					Confidence:   1.0,
					Provenance:   domain.ProvenanceObserved,
					DiscoveredAt: now,
					UpdatedAt:    now,
				})
			}

			// Relate NAT Gateway -> Elastic IP
			for _, addr := range nat.NatGatewayAddresses {
				if addr.AllocationId != nil && *addr.AllocationId != "" {
					eipARN := fmt.Sprintf("arn:aws:ec2:%s:%s:elastic-ip/%s", region, account.AccountID, *addr.AllocationId)
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: entity.ID,
						TargetARN:        eipARN,
						TargetNativeID:   *addr.AllocationId,
						RelationshipType: domain.RelAttachedTo,
						Properties: map[string]interface{}{
							"allocation_id": *addr.AllocationId,
						},
						Confidence:   1.0,
						Provenance:   domain.ProvenanceObserved,
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				}
			}
		}
	}

	return entities, relationships, nil
}
