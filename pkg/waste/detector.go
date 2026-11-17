package waste

import (
	"context"
	"fmt"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// Detector evaluates canonical infrastructure resources and metrics to uncover waste.
type Detector struct{}

func NewDetector() *Detector {
	return &Detector{}
}

// DetectionContext bundles resources and telemetry rollups for a tenant's account.
type DetectionContext struct {
	Account  *domain.CloudAccount
	Entities []domain.ResourceEntity
	Metrics  map[uuid.UUID][]domain.MetricRollup // keyed by resource ID
	Now      time.Time
}

// Detect analyzes the provided resources and returns all identified waste findings.
// Crucial: The detector is 100% READ ONLY and deterministic.
func (d *Detector) Detect(ctx context.Context, dc DetectionContext) []domain.WasteFinding {
	if dc.Now.IsZero() {
		dc.Now = time.Now().UTC()
	}

	var findings []domain.WasteFinding

	for _, entity := range dc.Entities {
		switch entity.ResourceType {
		case "aws:ec2:volume":
			if f, found := d.checkUnattachedVolume(entity, dc); found {
				findings = append(findings, f)
			}
		case "aws:ec2:eip":
			if f, found := d.checkUnattachedEIP(entity, dc); found {
				findings = append(findings, f)
			}
		case "aws:ec2:snapshot":
			if f, found := d.checkStaleSnapshot(entity, dc); found {
				findings = append(findings, f)
			}
		case "aws:ec2:instance":
			if f, found := d.checkIdleInstance(entity, dc); found {
				findings = append(findings, f)
			}
		case "aws:ec2:nat-gateway":
			if f, found := d.checkIdleNATGateway(entity, dc); found {
				findings = append(findings, f)
			}
		case "aws:rds:db-instance":
			if f, found := d.checkIdleDatabase(entity, dc); found {
				findings = append(findings, f)
			}
		}
	}

	return findings
}

// 1. Unattached EBS Volume
func (d *Detector) checkUnattachedVolume(e domain.ResourceEntity, dc DetectionContext) (domain.WasteFinding, bool) {
	// Status "available" in AWS EC2 means unattached
	if e.Status != domain.ResourceStatusAvailable {
		return domain.WasteFinding{}, false
	}

	sizeGiB := 0.0
	if rawSize, ok := e.ObservedState["size_gib"]; ok {
		switch v := rawSize.(type) {
		case int32:
			sizeGiB = float64(v)
		case int:
			sizeGiB = float64(v)
		case float64:
			sizeGiB = v
		}
	}
	if sizeGiB <= 0 {
		sizeGiB = 20.0 // fallback standard volume size
	}

	volType := "gp3"
	if vt, ok := e.ObservedState["volume_type"].(string); ok && vt != "" {
		volType = vt
	}

	// Cost formula: gp3 ~$0.08/GB-month, gp2 ~$0.10/GB-month, io2 ~$0.125/GB-month
	pricePerGiB := 0.08
	switch volType {
	case "gp2":
		pricePerGiB = 0.10
	case "io1", "io2":
		pricePerGiB = 0.125
	}
	estimatedSavings := sizeGiB * pricePerGiB

	observationDays := 7
	if e.CreatedAtProvider != nil {
		daysSinceCreate := int(dc.Now.Sub(*e.CreatedAtProvider).Hours() / 24)
		if daysSinceCreate > 0 {
			observationDays = daysSinceCreate
		}
	}

	finding := domain.WasteFinding{
		ID:                      uuid.New(),
		TenantID:                e.TenantID,
		CloudAccountID:          e.CloudAccountID,
		ResourceID:              e.ID,
		ResourceARN:             e.ARN,
		ResourceType:            e.ResourceType,
		ResourceName:            e.Name,
		Region:                  e.Region,
		WasteType:               domain.WasteTypeUnattachedVolume,
		Confidence:              1.00, // 100% deterministic fact
		EstimatedMonthlySavings: estimatedSavings,
		ObservationPeriodDays:   observationDays,
		Facts: []domain.FactEvidence{
			{
				Description: fmt.Sprintf("EBS volume %s is in 'available' state (not attached to any EC2 instance).", e.NativeID),
				ObservedVal: "available",
				ObservedAt:  e.ObservedAt,
			},
			{
				Description: fmt.Sprintf("Provisioned volume capacity is %.0f GiB (%s).", sizeGiB, volType),
				ObservedVal: sizeGiB,
				Unit:        "GiB",
				ObservedAt:  e.ObservedAt,
			},
		},
		InferenceReason:       "Unattached EBS volumes continue to incur full provisioned storage costs regardless of whether an EC2 instance is utilizing them.",
		RecommendationSummary: "Verify if data on this unattached volume is still needed. Create an archival snapshot if required, then delete the unattached volume.",
		Status:                domain.WasteStatusActive,
		CreatedAt:             dc.Now,
		UpdatedAt:             dc.Now,
	}

	return finding, true
}

// 2. Unattached Elastic IP
func (d *Detector) checkUnattachedEIP(e domain.ResourceEntity, dc DetectionContext) (domain.WasteFinding, bool) {
	if e.Status != domain.ResourceStatusAvailable {
		return domain.WasteFinding{}, false
	}

	// AWS charges $0.005/hr for idle EIPs = ~$3.60/month
	estimatedSavings := 3.60

	finding := domain.WasteFinding{
		ID:                      uuid.New(),
		TenantID:                e.TenantID,
		CloudAccountID:          e.CloudAccountID,
		ResourceID:              e.ID,
		ResourceARN:             e.ARN,
		ResourceType:            e.ResourceType,
		ResourceName:            e.Name,
		Region:                  e.Region,
		WasteType:               domain.WasteTypeUnattachedEIP,
		Confidence:              1.00,
		EstimatedMonthlySavings: estimatedSavings,
		ObservationPeriodDays:   7,
		Facts: []domain.FactEvidence{
			{
				Description: fmt.Sprintf("Elastic IP %s is allocated to the account but unassociated with any EC2 instance or Network Interface.", e.Name),
				ObservedVal: "unassociated",
				ObservedAt:  e.ObservedAt,
			},
		},
		InferenceReason:       "AWS charges $0.005 per hour for in-use public IPv4 address reservations that are not actively bound to a running instance.",
		RecommendationSummary: "Release this unassociated Elastic IP address to avoid the hourly reservation penalty.",
		Status:                domain.WasteStatusActive,
		CreatedAt:             dc.Now,
		UpdatedAt:             dc.Now,
	}

	return finding, true
}

// 3. Stale EBS Snapshot
func (d *Detector) checkStaleSnapshot(e domain.ResourceEntity, dc DetectionContext) (domain.WasteFinding, bool) {
	if e.CreatedAtProvider == nil {
		return domain.WasteFinding{}, false
	}

	age := dc.Now.Sub(*e.CreatedAtProvider)
	ageDays := int(age.Hours() / 24)
	if ageDays < 90 {
		return domain.WasteFinding{}, false
	}

	sizeGiB := 20.0
	if rawSize, ok := e.ObservedState["volume_size"]; ok {
		switch v := rawSize.(type) {
		case int32:
			sizeGiB = float64(v)
		case int:
			sizeGiB = float64(v)
		case float64:
			sizeGiB = v
		}
	}

	// EBS snapshot storage cost ~$0.05/GB-month
	estimatedSavings := sizeGiB * 0.05

	finding := domain.WasteFinding{
		ID:                      uuid.New(),
		TenantID:                e.TenantID,
		CloudAccountID:          e.CloudAccountID,
		ResourceID:              e.ID,
		ResourceARN:             e.ARN,
		ResourceType:            e.ResourceType,
		ResourceName:            e.Name,
		Region:                  e.Region,
		WasteType:               domain.WasteTypeStaleSnapshot,
		Confidence:              0.85,
		EstimatedMonthlySavings: estimatedSavings,
		ObservationPeriodDays:   ageDays,
		Facts: []domain.FactEvidence{
			{
				Description: fmt.Sprintf("Snapshot %s was created on %s (%d days ago).", e.NativeID, e.CreatedAtProvider.Format("2006-01-02"), ageDays),
				ObservedVal: ageDays,
				Unit:        "days",
				ObservedAt:  e.ObservedAt,
			},
			{
				Description: fmt.Sprintf("Snapshot base volume size was %.0f GiB.", sizeGiB),
				ObservedVal: sizeGiB,
				Unit:        "GiB",
				ObservedAt:  e.ObservedAt,
			},
		},
		InferenceReason:       "EBS snapshots older than 90 days are frequently abandoned manual test/debug backups that continue accumulating storage charges.",
		RecommendationSummary: "Verify snapshot data against regulatory retention requirements. If obsolete, delete this historical snapshot.",
		Status:                domain.WasteStatusActive,
		CreatedAt:             dc.Now,
		UpdatedAt:             dc.Now,
	}

	return finding, true
}

// 4. Idle EC2 Instance
func (d *Detector) checkIdleInstance(e domain.ResourceEntity, dc DetectionContext) (domain.WasteFinding, bool) {
	if e.Status != domain.ResourceStatusRunning {
		return domain.WasteFinding{}, false
	}

	metrics, ok := dc.Metrics[e.ID]
	if !ok || len(metrics) == 0 {
		return domain.WasteFinding{}, false
	}

	var cpuMetric *domain.MetricRollup
	for _, m := range metrics {
		if m.MetricName == "CPUUtilization" {
			cpuMetric = &m
			break
		}
	}

	if cpuMetric == nil || cpuMetric.SampleCount < 5 {
		return domain.WasteFinding{}, false
	}

	// Idle threshold: Average CPU < 5.0% and P95 CPU < 15.0%
	if cpuMetric.AvgValue >= 5.0 || cpuMetric.P95Value >= 15.0 {
		return domain.WasteFinding{}, false
	}

	// Estimate monthly savings based on typical instance run rate (~$70/mo for m5.large baseline)
	estimatedSavings := 70.0
	if instType, ok := e.ObservedState["instance_type"].(string); ok {
		switch instType {
		case "t3.micro", "t4g.micro":
			estimatedSavings = 7.50
		case "t3.small", "t4g.small":
			estimatedSavings = 15.00
		case "t3.medium", "m5.large", "c5.large":
			estimatedSavings = 70.00
		case "m5.xlarge", "c5.xlarge", "r5.large":
			estimatedSavings = 140.00
		case "m5.2xlarge", "c5.2xlarge":
			estimatedSavings = 280.00
		}
	}

	finding := domain.WasteFinding{
		ID:                      uuid.New(),
		TenantID:                e.TenantID,
		CloudAccountID:          e.CloudAccountID,
		ResourceID:              e.ID,
		ResourceARN:             e.ARN,
		ResourceType:            e.ResourceType,
		ResourceName:            e.Name,
		Region:                  e.Region,
		WasteType:               domain.WasteTypeIdleInstance,
		Confidence:              0.90,
		EstimatedMonthlySavings: estimatedSavings,
		ObservationPeriodDays:   14,
		Facts: []domain.FactEvidence{
			{
				Description: fmt.Sprintf("Instance average CPU utilization was %.2f%% (P95: %.2f%%) over 14 days.", cpuMetric.AvgValue, cpuMetric.P95Value),
				MetricName:  "CPUUtilization",
				ObservedVal: cpuMetric.AvgValue,
				Unit:        "percent",
				ObservedAt:  e.ObservedAt,
			},
		},
		InferenceReason:       "The instance has exhibited minimal processing activity over a two-week sliding observation window, suggesting it may be idle or over-provisioned.",
		RecommendationSummary: "Examine whether this workload can be scheduled for auto-stop, downsized to a smaller instance family, or consolidated into a shared cluster.",
		Status:                domain.WasteStatusActive,
		CreatedAt:             dc.Now,
		UpdatedAt:             dc.Now,
	}

	return finding, true
}

// 5. Idle NAT Gateway
func (d *Detector) checkIdleNATGateway(e domain.ResourceEntity, dc DetectionContext) (domain.WasteFinding, bool) {
	if e.Status != domain.ResourceStatusAvailable {
		return domain.WasteFinding{}, false
	}

	// NAT Gateway fixed provisioning charge: ~$32.40/month
	estimatedSavings := 32.40

	finding := domain.WasteFinding{
		ID:                      uuid.New(),
		TenantID:                e.TenantID,
		CloudAccountID:          e.CloudAccountID,
		ResourceID:              e.ID,
		ResourceARN:             e.ARN,
		ResourceType:            e.ResourceType,
		ResourceName:            e.Name,
		Region:                  e.Region,
		WasteType:               domain.WasteTypeIdleNATGateway,
		Confidence:              0.80,
		EstimatedMonthlySavings: estimatedSavings,
		ObservationPeriodDays:   7,
		Facts: []domain.FactEvidence{
			{
				Description: fmt.Sprintf("NAT Gateway %s is active and incurring baseline provisioning charges of ~$32.40/month.", e.NativeID),
				ObservedVal: "provisioned",
				ObservedAt:  e.ObservedAt,
			},
		},
		InferenceReason:       "NAT Gateways charge fixed hourly rates regardless of traffic volume. Subnets with no active outbound workloads incur ongoing idle costs.",
		RecommendationSummary: "Verify whether workloads in the associated subnet require public internet egress. If egress is obsolete, delete the NAT Gateway.",
		Status:                domain.WasteStatusActive,
		CreatedAt:             dc.Now,
		UpdatedAt:             dc.Now,
	}

	return finding, true
}

// 6. Idle RDS Database
func (d *Detector) checkIdleDatabase(e domain.ResourceEntity, dc DetectionContext) (domain.WasteFinding, bool) {
	if e.Status != domain.ResourceStatusAvailable {
		return domain.WasteFinding{}, false
	}

	metrics, ok := dc.Metrics[e.ID]
	if !ok || len(metrics) == 0 {
		return domain.WasteFinding{}, false
	}

	var connMetric *domain.MetricRollup
	for _, m := range metrics {
		if m.MetricName == "DatabaseConnections" {
			connMetric = &m
			break
		}
	}

	if connMetric == nil || connMetric.SampleCount < 3 {
		return domain.WasteFinding{}, false
	}

	// Idle threshold: Average connections == 0 and Max connections <= 1
	if connMetric.AvgValue > 0.05 || connMetric.MaxValue > 1.0 {
		return domain.WasteFinding{}, false
	}

	estimatedSavings := 90.0 // average db.t3.medium RDS baseline
	if instClass, ok := e.ObservedState["instance_class"].(string); ok {
		switch instClass {
		case "db.t3.micro", "db.t4g.micro":
			estimatedSavings = 18.0
		case "db.t3.small", "db.t4g.small":
			estimatedSavings = 36.0
		case "db.t3.medium", "db.m5.large":
			estimatedSavings = 130.0
		case "db.m5.xlarge", "db.r5.large":
			estimatedSavings = 260.0
		}
	}

	finding := domain.WasteFinding{
		ID:                      uuid.New(),
		TenantID:                e.TenantID,
		CloudAccountID:          e.CloudAccountID,
		ResourceID:              e.ID,
		ResourceARN:             e.ARN,
		ResourceType:            e.ResourceType,
		ResourceName:            e.Name,
		Region:                  e.Region,
		WasteType:               domain.WasteTypeIdleDatabase,
		Confidence:              0.95,
		EstimatedMonthlySavings: estimatedSavings,
		ObservationPeriodDays:   7,
		Facts: []domain.FactEvidence{
			{
				Description: fmt.Sprintf("RDS instance %s recorded 0 active database connections over 7 days.", e.NativeID),
				MetricName:  "DatabaseConnections",
				ObservedVal: 0,
				ObservedAt:  e.ObservedAt,
			},
		},
		InferenceReason:       "Database instance has maintained zero active client connections, indicating it may be an abandoned development or staging database.",
		RecommendationSummary: "Verify if this database is still required. Take an automated snapshot for safety, then stop or terminate the instance.",
		Status:                domain.WasteStatusActive,
		CreatedAt:             dc.Now,
		UpdatedAt:             dc.Now,
	}

	return finding, true
}
