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

// DiscoverELBv2 catalogs Application and Network Load Balancers and Target Groups.
func DiscoverELBv2(ctx context.Context, client *elasticloadbalancingv2.Client, account *domain.CloudAccount, region string) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	var entities []domain.ResourceEntity
	var relationships []domain.ResourceRelationship
	now := time.Now().UTC()

	// 1. Load Balancers (Paginated)
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
		}
	}

	return entities, relationships, nil
}
