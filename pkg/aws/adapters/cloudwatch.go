package adapters

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwTypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// MetricQuerySpec defines parameters for fetching CloudWatch telemetry.
type MetricQuerySpec struct {
	Namespace      string
	MetricName     string
	DimensionName  string
	DimensionValue string
	WindowDays     int
	Unit           string
}

// FetchMetricRollup retrieves metric datapoints from CloudWatch over a window and calculates statistical rollups.
func FetchMetricRollup(ctx context.Context, client *cloudwatch.Client, tenantID uuid.UUID, resourceID uuid.UUID, spec MetricQuerySpec) (*domain.MetricRollup, error) {
	if client == nil {
		return nil, fmt.Errorf("cloudwatch client is nil")
	}

	endTime := time.Now().UTC()
	startTime := endTime.Add(-time.Duration(spec.WindowDays) * 24 * time.Hour)

	// Period in seconds: 1 hour (3600s) for windows up to 14 days, 6 hours for longer
	period := int32(3600)
	if spec.WindowDays > 14 {
		period = 21600
	}

	input := &cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String(spec.Namespace),
		MetricName: aws.String(spec.MetricName),
		Dimensions: []cwTypes.Dimension{
			{
				Name:  aws.String(spec.DimensionName),
				Value: aws.String(spec.DimensionValue),
			},
		},
		StartTime: aws.Time(startTime),
		EndTime:   aws.Time(endTime),
		Period:    aws.Int32(period),
		Statistics: []cwTypes.Statistic{
			cwTypes.StatisticAverage,
			cwTypes.StatisticMaximum,
			cwTypes.StatisticMinimum,
			cwTypes.StatisticSampleCount,
		},
	}

	output, err := client.GetMetricStatistics(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed GetMetricStatistics for %s/%s: %w", spec.Namespace, spec.MetricName, err)
	}

	rollup := &domain.MetricRollup{
		ID:          uuid.New(),
		TenantID:    tenantID,
		ResourceID:  resourceID,
		MetricName:  spec.MetricName,
		PeriodStart: startTime,
		PeriodEnd:   endTime,
		Unit:        spec.Unit,
	}

	if len(output.Datapoints) == 0 {
		return rollup, nil
	}

	var sum float64
	var minVal = aws.ToFloat64(output.Datapoints[0].Minimum)
	var maxVal = aws.ToFloat64(output.Datapoints[0].Maximum)
	var averages []float64

	for _, dp := range output.Datapoints {
		val := aws.ToFloat64(dp.Average)
		averages = append(averages, val)
		sum += val

		if aws.ToFloat64(dp.Minimum) < minVal {
			minVal = aws.ToFloat64(dp.Minimum)
		}
		if aws.ToFloat64(dp.Maximum) > maxVal {
			maxVal = aws.ToFloat64(dp.Maximum)
		}
		rollup.SampleCount++
	}

	rollup.AvgValue = sum / float64(len(output.Datapoints))
	rollup.MinValue = minVal
	rollup.MaxValue = maxVal

	// Calculate P95
	sort.Float64s(averages)
	p95Idx := int(float64(len(averages)) * 0.95)
	if p95Idx >= len(averages) {
		p95Idx = len(averages) - 1
	}
	rollup.P95Value = averages[p95Idx]

	return rollup, nil
}
