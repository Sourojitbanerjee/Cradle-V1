package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	cradleAWS "github.com/cradle-dev/cradle/pkg/aws"
	"github.com/cradle-dev/cradle/pkg/cost"
	"github.com/cradle-dev/cradle/pkg/discovery"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/graph"
	"github.com/cradle-dev/cradle/pkg/provider"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/cradle-dev/cradle/pkg/telemetry"
	"github.com/cradle-dev/cradle/pkg/waste"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const WorkerVersion = "0.1.0-alpha"

func main() {
	logger := telemetry.NewLogger(os.Stdout, slog.LevelInfo)
	slog.SetDefault(logger)

	dbURL := os.Getenv("DATABASE_URL")
	logger.Info("Starting Cradle Background Discovery Worker",
		slog.String("version", WorkerVersion),
		slog.Int("pid", os.Getpid()),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		s := <-sig
		logger.Info("Received termination signal; draining discovery tasks...", slog.String("signal", s.String()))
		cancel()
	}()

	var pool *pgxpool.Pool
	if dbURL != "" {
		poolCtx, poolCancel := context.WithTimeout(ctx, 10*time.Second)
		var err error
		pool, err = postgres.NewPool(poolCtx, postgres.Config{URL: dbURL})
		poolCancel()
		if err != nil {
			logger.Error("Failed to connect to PostgreSQL database", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		logger.Info("Worker connected to PostgreSQL pool")
	} else {
		logger.Warn("DATABASE_URL not set; worker running in idle monitoring mode")
	}

	// Register AWS provider adapter
	awsFactory, err := cradleAWS.NewClientFactory(ctx)
	if err != nil {
		logger.Warn("Could not load default AWS credentials; provider will use runtime assumed sessions", slog.String("error", err.Error()))
	} else {
		provider.DefaultRegistry.Register(cradleAWS.NewAWSProvider(awsFactory))
		logger.Info("Worker registered AWS cloud provider adapter")
	}

	orchestrator := discovery.NewOrchestrator(pool, provider.DefaultRegistry)
	costEngine := cost.NewEngine(pool)
	queryEngine := graph.NewQueryEngine(pool)
	detector := waste.NewDetector()

	pollInterval := 60 * time.Second
	if intervalStr := os.Getenv("WORKER_POLL_INTERVAL"); intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			pollInterval = d
		}
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	logger.Info("Worker engine initialized; listening for cloud discovery tasks",
		slog.Duration("poll_interval", pollInterval),
	)

	for {
		select {
		case <-ctx.Done():
			logger.Info("Worker stopped cleanly; all tasks drained")
			return
		case <-ticker.C:
			if pool == nil {
				logger.Debug("Worker heartbeat check (no DB)", slog.Time("timestamp", time.Now().UTC()))
				continue
			}

			// Scan for connected accounts that need synchronization
			accounts, err := fetchAccountsDueForSync(ctx, pool)
			if err != nil {
				logger.Error("Worker failed querying accounts due for sync", slog.String("error", err.Error()))
				continue
			}

			for _, acc := range accounts {
				if ctx.Err() != nil {
					return
				}

				logger.Info("Starting scheduled discovery sync for account",
					slog.String("account_id", acc.AccountID),
					slog.String("tenant_id", acc.TenantID.String()),
				)

				result, err := orchestrator.RunDiscovery(ctx, &acc)
				if err != nil {
					logger.Error("Scheduled discovery failed for account",
						slog.String("account_id", acc.AccountID),
						slog.String("error", err.Error()),
					)
					continue
				}

				logger.Info("Scheduled discovery completed for account",
					slog.String("account_id", acc.AccountID),
					slog.Int("discovered_count", len(result.Entities)),
				)

				// Run post-discovery waste analysis
				runWasteAnalysis(ctx, pool, queryEngine, costEngine, detector, &acc, logger)
			}
		}
	}
}

func fetchAccountsDueForSync(ctx context.Context, pool *pgxpool.Pool) ([]domain.CloudAccount, error) {
	query := `
		SELECT id, tenant_id, provider, account_id, name, role_arn, external_id, regions, status, last_discovered_at, created_at, updated_at
		FROM cloud_accounts
		WHERE status = 'connected'
		  AND (last_discovered_at IS NULL OR last_discovered_at < NOW() - INTERVAL '1 hour')
		ORDER BY last_discovered_at ASC NULLS FIRST
		LIMIT 10;
	`
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.CloudAccount
	for rows.Next() {
		var acc domain.CloudAccount
		var provStr string
		var statStr string
		if err := rows.Scan(
			&acc.ID, &acc.TenantID, &provStr, &acc.AccountID, &acc.Name,
			&acc.RoleARN, &acc.ExternalID, &acc.Regions, &statStr,
			&acc.LastDiscoveredAt, &acc.CreatedAt, &acc.UpdatedAt,
		); err != nil {
			return nil, err
		}
		acc.Provider = domain.ProviderType(provStr)
		acc.Status = domain.AccountStatus(statStr)
		accounts = append(accounts, acc)
	}
	return accounts, rows.Err()
}

func runWasteAnalysis(
	ctx context.Context,
	pool *pgxpool.Pool,
	qe *graph.QueryEngine,
	ce *cost.Engine,
	detector *waste.Detector,
	acc *domain.CloudAccount,
	logger *slog.Logger,
) {
	resResult, err := qe.ListResources(ctx, graph.ResourceFilter{
		TenantID: acc.TenantID,
		Limit:    500,
	})
	if err != nil {
		logger.Error("Failed querying resources for post-sync waste analysis", slog.String("error", err.Error()))
		return
	}

	resourceIDs := make([]uuid.UUID, 0, len(resResult.Resources))
	for _, res := range resResult.Resources {
		resourceIDs = append(resourceIDs, res.ID)
	}

	metricsMap, err := ce.GetMetricRollups(ctx, acc.TenantID, resourceIDs)
	if err != nil {
		logger.Error("Failed querying metrics for post-sync waste analysis", slog.String("error", err.Error()))
		metricsMap = make(map[uuid.UUID][]domain.MetricRollup)
	}

	dc := waste.DetectionContext{
		Account:  acc,
		Entities: resResult.Resources,
		Metrics:  metricsMap,
		Now:      time.Now().UTC(),
	}

	findings := detector.Detect(ctx, dc)
	if len(findings) > 0 {
		if err := ce.SaveWasteFindings(ctx, acc.TenantID, findings); err != nil {
			logger.Error("Failed saving post-sync waste findings", slog.String("error", err.Error()))
			return
		}
		logger.Info("Waste analysis completed successfully",
			slog.String("account_id", acc.AccountID),
			slog.Int("findings_count", len(findings)),
		)
	}
}
