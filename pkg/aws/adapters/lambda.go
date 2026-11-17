package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// DiscoverLambda catalogs Lambda functions in the target region.
func DiscoverLambda(ctx context.Context, client *lambda.Client, account *domain.CloudAccount, region string) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	var entities []domain.ResourceEntity
	now := time.Now().UTC()

	fnPaginator := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})
	for fnPaginator.HasMorePages() {
		page, err := fnPaginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed ListFunctions in %s: %w", region, err)
		}

		for _, fn := range page.Functions {
			fnName := aws.ToString(fn.FunctionName)
			arn := aws.ToString(fn.FunctionArn)

			observed := map[string]interface{}{
				"function_name": fnName,
				"runtime":       string(fn.Runtime),
				"memory_size":   aws.ToInt32(fn.MemorySize),
				"timeout_secs":  aws.ToInt32(fn.Timeout),
				"code_size":     fn.CodeSize,
				"handler":       aws.ToString(fn.Handler),
				"architecture":  fn.Architectures,
				"package_type":  string(fn.PackageType),
				"role_arn":      aws.ToString(fn.Role),
			}

			if fn.VpcConfig != nil {
				observed["vpc_id"] = aws.ToString(fn.VpcConfig.VpcId)
				observed["subnet_ids"] = fn.VpcConfig.SubnetIds
				observed["security_group_ids"] = fn.VpcConfig.SecurityGroupIds
			}

			entity := domain.ResourceEntity{
				ID:             uuid.New(),
				TenantID:       account.TenantID,
				CloudAccountID: account.ID,
				Provider:       domain.ProviderAWS,
				AccountID:      account.AccountID,
				Region:         region,
				ResourceType:   "aws:lambda:function",
				NativeID:       fnName,
				ARN:            arn,
				Name:           fnName,
				Status:         domain.ResourceStatusAvailable,
				Tags:           make(map[string]string),
				ObservedAt:     now,
				ObservedState:  observed,
				DerivedState: map[string]interface{}{
					"is_vpc_connected": fn.VpcConfig != nil && fn.VpcConfig.VpcId != nil,
				},
			}
			_ = entity.ComputeConfigurationHash()
			entities = append(entities, entity)
		}
	}

	return entities, nil, nil
}
