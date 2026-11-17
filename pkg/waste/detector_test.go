package waste

import (
	"context"
	"testing"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestDetectUnattachedVolume(t *testing.T) {
	detector := NewDetector()
	tenantID := uuid.New()
	accountID := uuid.New()
	now := time.Now().UTC()

	// 1. Unattached 100 GiB gp3 volume -> should detect waste
	unattachedVol := domain.ResourceEntity{
		ID:             uuid.New(),
		TenantID:       tenantID,
		CloudAccountID: accountID,
		Provider:       domain.ProviderAWS,
		ResourceType:   "aws:ec2:volume",
		NativeID:       "vol-0123456789abcdef0",
		ARN:            "arn:aws:ec2:us-east-1:123456789012:volume/vol-0123456789abcdef0",
		Name:           "abandoned-temp-storage",
		Status:         domain.ResourceStatusAvailable, // Available = unattached
		ObservedAt:     now,
		ObservedState: map[string]interface{}{
			"size_gib":    int32(100),
			"volume_type": "gp3",
		},
	}

	// 2. Attached in-use volume -> should NOT detect waste
	attachedVol := domain.ResourceEntity{
		ID:             uuid.New(),
		TenantID:       tenantID,
		CloudAccountID: accountID,
		Provider:       domain.ProviderAWS,
		ResourceType:   "aws:ec2:volume",
		NativeID:       "vol-attached",
		ARN:            "arn:aws:ec2:us-east-1:123456789012:volume/vol-attached",
		Status:         domain.ResourceStatusInUse, // InUse = attached
		ObservedAt:     now,
		ObservedState: map[string]interface{}{
			"size_gib":    int32(50),
			"volume_type": "gp3",
		},
	}

	dc := DetectionContext{
		Entities: []domain.ResourceEntity{unattachedVol, attachedVol},
		Now:      now,
	}

	findings := detector.Detect(context.Background(), dc)
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding for unattached volume, got %d", len(findings))
	}

	f := findings[0]
	if f.WasteType != domain.WasteTypeUnattachedVolume {
		t.Errorf("expected waste type %s, got %s", domain.WasteTypeUnattachedVolume, f.WasteType)
	}
	// 100 GiB gp3 * $0.08 = $8.00/month
	if f.EstimatedMonthlySavings != 8.00 {
		t.Errorf("expected estimated monthly savings of 8.00, got %.2f", f.EstimatedMonthlySavings)
	}
	if len(f.Facts) == 0 {
		t.Error("waste finding must cite observed facts")
	}
	if f.Confidence != 1.00 {
		t.Errorf("expected confidence 1.00 for unattached volume, got %.2f", f.Confidence)
	}
}

func TestDetectUnattachedEIP(t *testing.T) {
	detector := NewDetector()
	tenantID := uuid.New()
	accountID := uuid.New()
	now := time.Now().UTC()

	eip := domain.ResourceEntity{
		ID:             uuid.New(),
		TenantID:       tenantID,
		CloudAccountID: accountID,
		Provider:       domain.ProviderAWS,
		ResourceType:   "aws:ec2:eip",
		NativeID:       "eipalloc-12345",
		ARN:            "arn:aws:ec2:us-east-1:123456789012:elastic-ip/eipalloc-12345",
		Name:           "54.210.12.34",
		Status:         domain.ResourceStatusAvailable, // Available = unattached
		ObservedAt:     now,
	}

	dc := DetectionContext{
		Entities: []domain.ResourceEntity{eip},
		Now:      now,
	}

	findings := detector.Detect(context.Background(), dc)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for unattached EIP, got %d", len(findings))
	}
	if findings[0].WasteType != domain.WasteTypeUnattachedEIP {
		t.Errorf("expected %s, got %s", domain.WasteTypeUnattachedEIP, findings[0].WasteType)
	}
	if findings[0].EstimatedMonthlySavings != 3.60 {
		t.Errorf("expected $3.60/mo savings for idle EIP, got %.2f", findings[0].EstimatedMonthlySavings)
	}
}

func TestDetectStaleSnapshot(t *testing.T) {
	detector := NewDetector()
	tenantID := uuid.New()
	accountID := uuid.New()
	now := time.Now().UTC()

	oldDate := now.Add(-120 * 24 * time.Hour)  // 120 days old -> STALE
	freshDate := now.Add(-10 * 24 * time.Hour) // 10 days old -> FRESH

	staleSnap := domain.ResourceEntity{
		ID:                uuid.New(),
		TenantID:          tenantID,
		CloudAccountID:    accountID,
		Provider:          domain.ProviderAWS,
		ResourceType:      "aws:ec2:snapshot",
		NativeID:          "snap-old",
		ARN:               "arn:aws:ec2:us-east-1:123456789012:snapshot/snap-old",
		Name:              "backup-2025",
		Status:            domain.ResourceStatusAvailable,
		CreatedAtProvider: &oldDate,
		ObservedAt:        now,
		ObservedState: map[string]interface{}{
			"volume_size": int32(200),
		},
	}

	freshSnap := domain.ResourceEntity{
		ID:                uuid.New(),
		TenantID:          tenantID,
		CloudAccountID:    accountID,
		Provider:          domain.ProviderAWS,
		ResourceType:      "aws:ec2:snapshot",
		NativeID:          "snap-fresh",
		ARN:               "arn:aws:ec2:us-east-1:123456789012:snapshot/snap-fresh",
		Status:            domain.ResourceStatusAvailable,
		CreatedAtProvider: &freshDate,
		ObservedAt:        now,
	}

	dc := DetectionContext{
		Entities: []domain.ResourceEntity{staleSnap, freshSnap},
		Now:      now,
	}

	findings := detector.Detect(context.Background(), dc)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for stale snapshot, got %d", len(findings))
	}
	// 200 GiB * $0.05/GB = $10.00/mo
	if findings[0].EstimatedMonthlySavings != 10.00 {
		t.Errorf("expected $10.00/mo savings, got %.2f", findings[0].EstimatedMonthlySavings)
	}
}

func TestDetectIdleEC2Instance(t *testing.T) {
	detector := NewDetector()
	tenantID := uuid.New()
	accountID := uuid.New()
	now := time.Now().UTC()
	instanceID := uuid.New()

	instance := domain.ResourceEntity{
		ID:             instanceID,
		TenantID:       tenantID,
		CloudAccountID: accountID,
		Provider:       domain.ProviderAWS,
		ResourceType:   "aws:ec2:instance",
		NativeID:       "i-idle12345",
		ARN:            "arn:aws:ec2:us-east-1:123456789012:instance/i-idle12345",
		Name:           "staging-api-server",
		Status:         domain.ResourceStatusRunning,
		ObservedAt:     now,
		ObservedState: map[string]interface{}{
			"instance_type": "m5.large",
		},
	}

	// 14-day CPU metrics showing 2.1% average and 6.5% P95 (well below 5% / 15% thresholds)
	metrics := map[uuid.UUID][]domain.MetricRollup{
		instanceID: {
			{
				MetricName:  "CPUUtilization",
				AvgValue:    2.1,
				P95Value:    6.5,
				MaxValue:    12.0,
				SampleCount: 336, // 14 days of hourly points
				Unit:        "percent",
			},
		},
	}

	dc := DetectionContext{
		Entities: []domain.ResourceEntity{instance},
		Metrics:  metrics,
		Now:      now,
	}

	findings := detector.Detect(context.Background(), dc)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for idle instance, got %d", len(findings))
	}
	f := findings[0]
	if f.WasteType != domain.WasteTypeIdleInstance {
		t.Errorf("expected %s, got %s", domain.WasteTypeIdleInstance, f.WasteType)
	}
	if f.EstimatedMonthlySavings != 70.00 {
		t.Errorf("expected $70.00/mo savings for m5.large, got %.2f", f.EstimatedMonthlySavings)
	}
	if len(f.Facts) == 0 || f.Facts[0].MetricName != "CPUUtilization" {
		t.Error("expected fact citing CPUUtilization metric")
	}
}

func TestDetectIdleRDS(t *testing.T) {
	detector := NewDetector()
	tenantID := uuid.New()
	accountID := uuid.New()
	now := time.Now().UTC()
	dbID := uuid.New()

	db := domain.ResourceEntity{
		ID:             dbID,
		TenantID:       tenantID,
		CloudAccountID: accountID,
		Provider:       domain.ProviderAWS,
		ResourceType:   "aws:rds:db-instance",
		NativeID:       "dev-postgres-cluster",
		ARN:            "arn:aws:rds:us-east-1:123456789012:db:dev-postgres-cluster",
		Name:           "dev-postgres-cluster",
		Status:         domain.ResourceStatusAvailable,
		ObservedAt:     now,
		ObservedState: map[string]interface{}{
			"instance_class": "db.m5.large",
		},
	}

	metrics := map[uuid.UUID][]domain.MetricRollup{
		dbID: {
			{
				MetricName:  "DatabaseConnections",
				AvgValue:    0.0,
				MaxValue:    0.0,
				SampleCount: 168,
			},
		},
	}

	dc := DetectionContext{
		Entities: []domain.ResourceEntity{db},
		Metrics:  metrics,
		Now:      now,
	}

	findings := detector.Detect(context.Background(), dc)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for idle RDS database, got %d", len(findings))
	}
	if findings[0].WasteType != domain.WasteTypeIdleDatabase {
		t.Errorf("expected %s, got %s", domain.WasteTypeIdleDatabase, findings[0].WasteType)
	}
	if findings[0].EstimatedMonthlySavings != 130.00 {
		t.Errorf("expected $130.00/mo savings for db.m5.large, got %.2f", findings[0].EstimatedMonthlySavings)
	}
}
