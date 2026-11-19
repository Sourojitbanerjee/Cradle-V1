package inventory

import (
	"reflect"
	"testing"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func testResource(tags map[string]string) domain.ResourceEntity {
	return domain.ResourceEntity{ID: uuid.New(), TenantID: uuid.New(), ResourceType: "aws:s3:bucket", Name: "payments-prod-critical", Tags: tags}
}

func testRule(resource domain.ResourceEntity, assignments domain.InventoryAssignments) domain.OwnershipRule {
	return domain.OwnershipRule{ID: uuid.New(), TenantID: resource.TenantID, Name: "Payments mapping", TagKey: "team", TagValue: "payments",
		Priority: 100, Enabled: true, Assignments: assignments}
}

func TestUnknownWithoutExplicitEvidence(t *testing.T) {
	resource := testResource(nil)
	op := Resolve(resource, nil, nil)
	for _, answer := range []domain.InventoryAnswer{op.Owner, op.Team, op.Application, op.Environment, op.Purpose, op.Criticality, op.Orphan} {
		if answer.Value != domain.Unknown || answer.Confidence != 0 || len(answer.Evidence) == 0 {
			t.Fatalf("fabricated answer: %+v", answer)
		}
	}
	if !op.MissingOwner || !op.Untagged {
		t.Fatal("unknown resource must be queued and untagged")
	}
}

func TestTagResolutionAndAnalysis(t *testing.T) {
	resource := testResource(map[string]string{"Owner": "ops@example.test", "Team": "payments", "App": "checkout", "Env": "PROD",
		"purpose": "Process payments", "criticality": "critical", "empty": " ", "aws:cloudformation:stack-name": "stack"})
	op := Resolve(resource, nil, nil)
	if op.Owner.Value != "ops@example.test" || op.Team.Value != "payments" || op.Application.Value != "checkout" || op.Environment.Value != "production" || op.Purpose.Value != "Process payments" || op.Criticality.Value != "critical" {
		t.Fatalf("incorrect tag answers: %+v", op)
	}
	if op.Owner.Source != string(domain.ProvenanceObservedTag) || op.MissingOwner || op.Untagged {
		t.Fatal("wrong ownership provenance or flags")
	}
	if op.TagAnalysis.UserTagCount != 6 || !reflect.DeepEqual(op.TagAnalysis.EmptyKeys, []string{"empty"}) {
		t.Fatalf("unexpected tag analysis: %+v", op.TagAnalysis)
	}
	resource.Tags = map[string]string{"aws:createdBy": "AWS", "owner": "", "purpose": "UNKNOWN", "env": "preview"}
	op = Resolve(resource, nil, nil)
	if op.Owner.Value != domain.Unknown || op.Purpose.Value != domain.Unknown || op.Environment.Value != domain.Unknown {
		t.Fatal("unknown/unsupported values must remain unknown")
	}
	resource.Tags = map[string]string{"aws:createdBy": "AWS", "owner": " "}
	if !Resolve(resource, nil, nil).Untagged {
		t.Fatal("reserved/empty tags are not useful user tags")
	}
}

func TestRulesPrecedenceAndConflicts(t *testing.T) {
	resource := testResource(map[string]string{"team": "payments", "env": "dev"})
	rule := testRule(resource, domain.InventoryAssignments{Team: "Payments", Environment: "production", Application: "Billing", Purpose: "Charge invoices"})
	op := Resolve(resource, []domain.OwnershipRule{rule}, nil)
	if op.Team.Value != "Payments" || op.Team.Source != "OWNERSHIP_RULE" || op.MissingOwner || op.Environment.Value != "production" || op.Application.Value != "Billing" || op.Purpose.Value != "Charge invoices" {
		t.Fatalf("rule mapping failed: %+v", op)
	}
	conflict := testRule(resource, domain.InventoryAssignments{Team: "Finance"})
	op = Resolve(resource, []domain.OwnershipRule{rule, conflict}, nil)
	if op.Team.Value != domain.Unknown || op.Team.Source != "CONFLICT" || !op.MissingOwner || len(op.Team.Candidates) != 2 {
		t.Fatalf("conflict not surfaced: %+v", op)
	}
	conflict.Priority++
	if got := Resolve(resource, []domain.OwnershipRule{rule, conflict}, nil).Team.Value; got != "Finance" {
		t.Fatalf("higher priority did not win: %s", got)
	}
	conflict.Enabled = false
	if got := Resolve(resource, []domain.OwnershipRule{rule, conflict}, nil).Team.Value; got != "Payments" {
		t.Fatalf("disabled rule applied: %s", got)
	}
	rule.TenantID = uuid.New()
	if got := Resolve(resource, []domain.OwnershipRule{rule}, nil).Team.Value; got != "payments" {
		t.Fatal("cross-tenant rule applied")
	}
	rule.TenantID = resource.TenantID
	rule.TagKey = "Team"
	if got := Resolve(resource, []domain.OwnershipRule{rule}, nil).Team.Value; got != "payments" {
		t.Fatal("rule tag keys must match exactly")
	}
}

func TestConflictingTagsDeterministic(t *testing.T) {
	resource := testResource(map[string]string{"team": "Payments", "Team": "Finance", "env": "prod", "environment": "production"})
	first := Resolve(resource, nil, nil)
	if first.Team.Value != domain.Unknown || first.Environment.Value != "production" {
		t.Fatal("tag ambiguity or normalized agreement incorrect")
	}
	for i := 0; i < 50; i++ {
		if !reflect.DeepEqual(first, Resolve(resource, nil, nil)) {
			t.Fatal("resolution depends on map iteration")
		}
	}
}

func TestOwnershipRecords(t *testing.T) {
	resource := testResource(map[string]string{"team": "payments"})
	rule := testRule(resource, domain.InventoryAssignments{Team: "Payments"})
	record := domain.OwnershipRecord{ID: uuid.New(), TenantID: resource.TenantID, ResourceID: resource.ID, OwnerType: domain.OwnerTypeTeam,
		OwnerID: "SRE", Source: domain.OwnershipSourceManual, Confidence: 1}
	if got := Resolve(resource, []domain.OwnershipRule{rule}, []domain.OwnershipRecord{record}).Team.Value; got != "SRE" {
		t.Fatal("manual ownership should win")
	}
	record.Source = domain.OwnershipSourceTagInferred
	if got := Resolve(resource, []domain.OwnershipRule{rule}, []domain.OwnershipRecord{record}).Team.Value; got != "Payments" {
		t.Fatal("stale unverified inference applied")
	}
	record.IsVerified = true
	if got := Resolve(resource, []domain.OwnershipRule{rule}, []domain.OwnershipRecord{record}).Team.Value; got != "SRE" {
		t.Fatal("verified ownership should win")
	}
	record.TenantID = uuid.New()
	if got := Resolve(resource, []domain.OwnershipRule{rule}, []domain.OwnershipRecord{record}).Team.Value; got != "Payments" {
		t.Fatal("foreign record applied")
	}
}

func TestOrphanDetection(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		status     domain.ResourceStatus
		state      map[string]interface{}
		want       string
	}{
		{"unattached volume", "aws:ec2:volume", domain.ResourceStatusAvailable, nil, "ORPHANED"},
		{"attached volume", "aws:ec2:volume", domain.ResourceStatusInUse, nil, "NOT_ORPHANED"},
		{"unknown volume", "aws:ec2:volume", domain.ResourceStatusUnknown, nil, domain.Unknown},
		{"unassociated EIP", "aws:ec2:eip", domain.ResourceStatusAvailable, map[string]interface{}{"instance_id": "", "network_interface_id": ""}, "ORPHANED"},
		{"EIP on NAT interface", "aws:ec2:eip", domain.ResourceStatusAvailable, map[string]interface{}{"instance_id": "", "network_interface_id": "eni-123"}, "NOT_ORPHANED"},
		{"EIP missing evidence", "aws:ec2:eip", domain.ResourceStatusAvailable, nil, domain.Unknown},
		{"independent bucket", "aws:s3:bucket", domain.ResourceStatusAvailable, nil, domain.Unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource := testResource(nil)
			resource.ResourceType, resource.Status, resource.ObservedState = tc.kind, tc.status, tc.state
			if got := Resolve(resource, nil, nil).Orphan.Value; got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRuleValidation(t *testing.T) {
	resource := testResource(nil)
	valid := testRule(resource, domain.InventoryAssignments{Team: "Payments"})
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*domain.OwnershipRule){
		func(r *domain.OwnershipRule) { r.TenantID = uuid.Nil },
		func(r *domain.OwnershipRule) { r.Name = " " },
		func(r *domain.OwnershipRule) { r.TagKey = "" },
		func(r *domain.OwnershipRule) { r.TagValue = " " },
		func(r *domain.OwnershipRule) { r.Priority = -1 },
		func(r *domain.OwnershipRule) { r.Priority = 10001 },
		func(r *domain.OwnershipRule) { r.Assignments = domain.InventoryAssignments{} },
		func(r *domain.OwnershipRule) { r.Assignments.Team = "UNKNOWN" },
		func(r *domain.OwnershipRule) { r.Assignments.Environment = "preview" },
		func(r *domain.OwnershipRule) { r.Assignments.Criticality = "urgent" },
	} {
		rule := valid
		mutate(&rule)
		if err := rule.Validate(); err == nil {
			t.Fatalf("invalid rule accepted: %+v", rule)
		}
	}
}

func TestViewsGroupingAndPagination(t *testing.T) {
	resources := []domain.ResourceEntity{}
	for _, env := range []string{"production", "staging", "development"} {
		resource := testResource(map[string]string{"team": "Payments", "app": "Billing", "env": env, "criticality": "critical"})
		resource.MonthlyCostEstimate = 120
		resource.Operational = Resolve(resource, nil, nil)
		resources = append(resources, resource)
	}
	orphan := testResource(nil)
	orphan.ResourceType, orphan.Status = "aws:ec2:volume", domain.ResourceStatusAvailable
	orphan.Operational = Resolve(orphan, nil, nil)
	resources = append(resources, orphan)
	summary := Summarize(resources, 100)
	if summary.Total != 4 || summary.UnknownOwnership != 1 || summary.Untagged != 1 || len(summary.Groups) != 4 || summary.Views["high-cost"] != 3 || summary.Views["orphaned"] != 1 || summary.Views["critical"] != 3 {
		t.Fatalf("incorrect summary: %+v", summary)
	}
	for _, view := range Views {
		filter := Filter{View: view, Limit: 1, Offset: 0, MinMonthlyCost: 100}
		result := FilterResources(resources, filter)
		if result.Total != summary.Views[view] || len(result.Resources) != 1 {
			t.Fatalf("view %s: %+v", view, result)
		}
	}
	result := FilterResources(resources, Filter{Team: "Payments", Application: "Billing", Environment: "production", Limit: 1})
	if result.Total != 1 {
		t.Fatal("group filters failed")
	}
	result = FilterResources(resources, Filter{Limit: 50, Offset: 100})
	if result.Total != 4 || len(result.Resources) != 0 {
		t.Fatal("out-of-range page loses count")
	}
	if MatchesView(orphan, "high-cost", 0) {
		t.Fatal("zero estimates are not evidence of high cost")
	}
}
