package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	ceTypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// FetchCostAndUsage retrieves cost breakdowns from AWS Cost Explorer over a date range.
func FetchCostAndUsage(ctx context.Context, client *costexplorer.Client, account *domain.CloudAccount, startDate, endDate string) ([]domain.CostSnapshot, error) {
	if client == nil {
		return nil, fmt.Errorf("costexplorer client is nil")
	}

	input := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &ceTypes.DateInterval{
			Start: aws.String(startDate),
			End:   aws.String(endDate),
		},
		Granularity: ceTypes.GranularityDaily,
		Metrics:     []string{"UnblendedCost", "UsageQuantity"},
		GroupBy: []ceTypes.GroupDefinition{
			{
				Type: ceTypes.GroupDefinitionTypeDimension,
				Key:  aws.String("SERVICE"),
			},
			{
				Type: ceTypes.GroupDefinitionTypeDimension,
				Key:  aws.String("USAGE_TYPE"),
			},
		},
	}

	output, err := client.GetCostAndUsage(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed GetCostAndUsage for account %s: %w", account.AccountID, err)
	}

	var snapshots []domain.CostSnapshot
	now := time.Now().UTC()

	for _, resultByTime := range output.ResultsByTime {
		periodStart, _ := time.Parse("2006-01-02", aws.ToString(resultByTime.TimePeriod.Start))
		periodEnd, _ := time.Parse("2006-01-02", aws.ToString(resultByTime.TimePeriod.End))

		for _, group := range resultByTime.Groups {
			serviceName := ""
			usageType := ""
			if len(group.Keys) > 0 {
				serviceName = group.Keys[0]
			}
			if len(group.Keys) > 1 {
				usageType = group.Keys[1]
			}

			amount := 0.0
			currency := "USD"
			usageQty := 0.0
			usageUnit := ""

			if costMetric, ok := group.Metrics["UnblendedCost"]; ok {
				if costMetric.Amount != nil {
					amount, _ = strconv.ParseFloat(*costMetric.Amount, 64)
				}
				if costMetric.Unit != nil {
					currency = *costMetric.Unit
				}
			}

			if qtyMetric, ok := group.Metrics["UsageQuantity"]; ok {
				if qtyMetric.Amount != nil {
					usageQty, _ = strconv.ParseFloat(*qtyMetric.Amount, 64)
				}
				if qtyMetric.Unit != nil {
					usageUnit = *qtyMetric.Unit
				}
			}

			// Omit zero cost entries to preserve database density
			if amount <= 0.0001 && usageQty <= 0.0001 {
				continue
			}

			snapshot := domain.CostSnapshot{
				ID:             uuid.New(),
				TenantID:       account.TenantID,
				CloudAccountID: account.ID,
				ServiceName:    serviceName,
				UsageType:      usageType,
				PeriodStart:    periodStart,
				PeriodEnd:      periodEnd,
				Amount:         amount,
				Currency:       currency,
				UsageQuantity:  usageQty,
				UsageUnit:      usageUnit,
				CreatedAt:      now,
			}
			snapshots = append(snapshots, snapshot)
		}
	}

	return snapshots, nil
}
