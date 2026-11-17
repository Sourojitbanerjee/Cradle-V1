package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// DiscoverEC2 discovers EC2 instances, EBS volumes, snapshots, and Elastic IPs.
func DiscoverEC2(ctx context.Context, client *ec2.Client, account *domain.CloudAccount, region string) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	var entities []domain.ResourceEntity
	var relationships []domain.ResourceRelationship
	now := time.Now().UTC()

	// -------------------------------------------------------------
	// 1. EC2 Instances (Paginated)
	// -------------------------------------------------------------
	instPaginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	for instPaginator.HasMorePages() {
		page, err := instPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeInstances in %s: %w", region, err)
		}

		for _, res := range page.Reservations {
			for _, inst := range res.Instances {
				instID := aws.ToString(inst.InstanceId)
				tags := parseEC2Tags(inst.Tags)
				name := tags["Name"]
				if name == "" {
					name = instID
				}

				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s", region, account.AccountID, instID)
				status := normalizeInstanceStatus(inst.State)

				var launchTime *time.Time
				if inst.LaunchTime != nil {
					t := *inst.LaunchTime
					launchTime = &t
				}

				observed := map[string]interface{}{
					"instance_id":       instID,
					"instance_type":     string(inst.InstanceType),
					"state":             string(inst.State.Name),
					"vpc_id":            aws.ToString(inst.VpcId),
					"subnet_id":         aws.ToString(inst.SubnetId),
					"private_ip":        aws.ToString(inst.PrivateIpAddress),
					"public_ip":         aws.ToString(inst.PublicIpAddress),
					"architecture":      string(inst.Architecture),
					"hypervisor":        string(inst.Hypervisor),
					"virtualization":    string(inst.VirtualizationType),
					"security_groups":   inst.SecurityGroups,
					"block_device_maps": inst.BlockDeviceMappings,
				}

				entity := domain.ResourceEntity{
					ID:                uuid.New(),
					TenantID:          account.TenantID,
					CloudAccountID:    account.ID,
					Provider:          domain.ProviderAWS,
					AccountID:         account.AccountID,
					Region:            region,
					ResourceType:      "aws:ec2:instance",
					NativeID:          instID,
					ARN:               arn,
					Name:              name,
					Status:            status,
					Tags:              tags,
					CreatedAtProvider: launchTime,
					ObservedAt:        now,
					ObservedState:     observed,
					DerivedState: map[string]interface{}{
						"is_vpc_attached": inst.VpcId != nil,
					},
				}
				_ = entity.ComputeConfigurationHash()
				entities = append(entities, entity)

				// Synthesize relationships
				if inst.SubnetId != nil {
					subnetARN := fmt.Sprintf("arn:aws:ec2:%s:%s:subnet/%s", region, account.AccountID, *inst.SubnetId)
					relationships = append(relationships, domain.ResourceRelationship{
						ID:               uuid.New(),
						TenantID:         account.TenantID,
						SourceResourceID: entity.ID,
						RelationshipType: domain.RelAttachedTo,
						Properties: map[string]interface{}{
							"target_arn": subnetARN,
							"subnet_id":  *inst.SubnetId,
						},
						DiscoveredAt: now,
						UpdatedAt:    now,
					})
				}
			}
		}
	}

	// -------------------------------------------------------------
	// 2. EBS Volumes (Paginated)
	// -------------------------------------------------------------
	volPaginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{})
	for volPaginator.HasMorePages() {
		page, err := volPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeVolumes in %s: %w", region, err)
		}

		for _, vol := range page.Volumes {
			volID := aws.ToString(vol.VolumeId)
			tags := parseEC2Tags(vol.Tags)
			name := tags["Name"]
			if name == "" {
				name = volID
			}

			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:volume/%s", region, account.AccountID, volID)

			status := domain.ResourceStatusAvailable
			if vol.State == ec2Types.VolumeStateInUse {
				status = domain.ResourceStatusInUse
			}

			var createTime *time.Time
			if vol.CreateTime != nil {
				t := *vol.CreateTime
				createTime = &t
			}

			observed := map[string]interface{}{
				"volume_id":   volID,
				"size_gib":    aws.ToInt32(vol.Size),
				"volume_type": string(vol.VolumeType),
				"encrypted":   aws.ToBool(vol.Encrypted),
				"kms_key_id":  aws.ToString(vol.KmsKeyId),
				"iops":        aws.ToInt32(vol.Iops),
				"throughput":  aws.ToInt32(vol.Throughput),
				"state":       string(vol.State),
				"attachments": vol.Attachments,
			}

			entity := domain.ResourceEntity{
				ID:                uuid.New(),
				TenantID:          account.TenantID,
				CloudAccountID:    account.ID,
				Provider:          domain.ProviderAWS,
				AccountID:         account.AccountID,
				Region:            region,
				ResourceType:      "aws:ec2:volume",
				NativeID:          volID,
				ARN:               arn,
				Name:              name,
				Status:            status,
				Tags:              tags,
				CreatedAtProvider: createTime,
				ObservedAt:        now,
				ObservedState:     observed,
				DerivedState: map[string]interface{}{
					"is_attached": len(vol.Attachments) > 0,
				},
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
		}
	}

	// -------------------------------------------------------------
	// 3. EBS Snapshots (Filtered to self-owned to avoid public snapshots!)
	// -------------------------------------------------------------
	snapPaginator := ec2.NewDescribeSnapshotsPaginator(client, &ec2.DescribeSnapshotsInput{
		OwnerIds: []string{"self"},
	})
	for snapPaginator.HasMorePages() {
		page, err := snapPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed DescribeSnapshots in %s: %w", region, err)
		}

		for _, snap := range page.Snapshots {
			snapID := aws.ToString(snap.SnapshotId)
			tags := parseEC2Tags(snap.Tags)
			name := tags["Name"]
			if name == "" {
				name = snapID
			}

			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:snapshot/%s", region, account.AccountID, snapID)

			status := domain.ResourceStatusAvailable
			if snap.State != ec2Types.SnapshotStateCompleted {
				status = domain.ResourceStatusDegraded
			}

			var startTime *time.Time
			if snap.StartTime != nil {
				t := *snap.StartTime
				startTime = &t
			}

			observed := map[string]interface{}{
				"snapshot_id": snapID,
				"volume_id":   aws.ToString(snap.VolumeId),
				"volume_size": aws.ToInt32(snap.VolumeSize),
				"encrypted":   aws.ToBool(snap.Encrypted),
				"state":       string(snap.State),
				"description": aws.ToString(snap.Description),
			}

			entity := domain.ResourceEntity{
				ID:                uuid.New(),
				TenantID:          account.TenantID,
				CloudAccountID:    account.ID,
				Provider:          domain.ProviderAWS,
				AccountID:         account.AccountID,
				Region:            region,
				ResourceType:      "aws:ec2:snapshot",
				NativeID:          snapID,
				ARN:               arn,
				Name:              name,
				Status:            status,
				Tags:              tags,
				CreatedAtProvider: startTime,
				ObservedAt:        now,
				ObservedState:     observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
		}
	}

	// -------------------------------------------------------------
	// 4. Elastic IP Addresses (EIPs)
	// -------------------------------------------------------------
	eips, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err == nil && eips != nil {
		for _, addr := range eips.Addresses {
			allocID := aws.ToString(addr.AllocationId)
			if allocID == "" {
				allocID = aws.ToString(addr.PublicIp)
			}
			tags := parseEC2Tags(addr.Tags)
			name := tags["Name"]
			if name == "" {
				name = aws.ToString(addr.PublicIp)
			}

			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:elastic-ip/%s", region, account.AccountID, allocID)
			status := domain.ResourceStatusAvailable
			if addr.InstanceId != nil {
				status = domain.ResourceStatusInUse
			}

			observed := map[string]interface{}{
				"allocation_id":        allocID,
				"public_ip":            aws.ToString(addr.PublicIp),
				"private_ip":           aws.ToString(addr.PrivateIpAddress),
				"instance_id":          aws.ToString(addr.InstanceId),
				"network_interface_id": aws.ToString(addr.NetworkInterfaceId),
			}

			entity := domain.ResourceEntity{
				ID:             uuid.New(),
				TenantID:       account.TenantID,
				CloudAccountID: account.ID,
				Provider:       domain.ProviderAWS,
				AccountID:      account.AccountID,
				Region:         region,
				ResourceType:   "aws:ec2:eip",
				NativeID:       allocID,
				ARN:            arn,
				Name:           name,
				Status:         status,
				Tags:           tags,
				ObservedAt:     now,
				ObservedState:  observed,
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
		}
	}

	return entities, relationships, nil
}

func parseEC2Tags(tags []ec2Types.Tag) map[string]string {
	result := make(map[string]string, len(tags))
	for _, t := range tags {
		if t.Key != nil && t.Value != nil {
			result[*t.Key] = *t.Value
		}
	}
	return result
}

func normalizeInstanceStatus(state *ec2Types.InstanceState) domain.ResourceStatus {
	if state == nil {
		return domain.ResourceStatusUnknown
	}
	switch state.Name {
	case ec2Types.InstanceStateNameRunning:
		return domain.ResourceStatusRunning
	case ec2Types.InstanceStateNameStopped:
		return domain.ResourceStatusStopped
	case ec2Types.InstanceStateNameTerminated:
		return domain.ResourceStatusTerminated
	default:
		return domain.ResourceStatusUnknown
	}
}
