package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/inventory"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const inventoryTestTags = `{"team":"payments","application":"Legacy","environment":"dev","purpose":"Current direct intent","criticality":"high"}`

func TestInventoryIntegration(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("PostgreSQL not available; skipping inventory integration tests")
	}
	t.Cleanup(pool.Close)
	router := newTestRouter(pool)

	// All SQL, including cleanup, is confined to freshly generated tenant IDs.
	primary := inventoryTestFixture(t, pool, 510)
	other := inventoryTestFixture(t, pool, 1)
	unknownID := inventoryTestSeedResource(t, pool, primary, "unknown", "aws:ec2:instance", "running", `{"aws:createdBy":"system","empty":""}`, 0, false)
	orphanID := inventoryTestSeedResource(t, pool, primary, "orphan", "aws:ec2:volume", "available", `{"team":"storage","application":"Archive","environment":"staging","purpose":"Retention","criticality":"critical"}`, 25, false)
	deletedID := inventoryTestSeedResource(t, pool, primary, "inventory-0000-deleted", "aws:ec2:instance", "terminated", inventoryTestTags, 999, true)
	foreignRuleID := uuid.New()
	inventoryTestSQL(t, pool, other.tenantID, `INSERT INTO ownership_rules
		(id, tenant_id, name, tag_key, tag_value, priority, enabled, assignments)
		VALUES ($1, $2, 'Private mapping', 'team', 'payments', 100, true,
		'{"team":"Foreign","application":"Private","environment":"production","purpose":"Private intent"}'::jsonb)`, foreignRuleID, other.tenantID)

	t.Run("operational answers and canonical resource enrichment", func(t *testing.T) {
		for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/resources"} {
			t.Run(base, func(t *testing.T) {
				resource := inventoryTestGet(t, router, primary.tenantID, base, primary.taggedID)
				inventoryTestAssertDirect(t, resource, "Legacy", "development", "Current direct intent")
				list := inventoryTestList(t, router, primary.tenantID, base+"?search=inventory-0001")
				inventoryTestAssertList(t, list, 1, 1, 50, 0)
				if list.Resources[0].ID != primary.taggedID || !reflect.DeepEqual(list.Resources[0].Operational, resource.Operational) {
					t.Fatalf("list/get operational mismatch: %+v", list.Resources[0])
				}
				inventoryTestAssertOperational(t, list.Resources[0])
				inventoryTestRequest(t, router, primary.tenantID, http.MethodGet, base+"/"+deletedID.String(), "", http.StatusNotFound)
				inventoryTestRequest(t, router, primary.tenantID, http.MethodGet, base+"/"+uuid.New().String(), "", http.StatusNotFound)
			})
		}
		unknown := inventoryTestGet(t, router, primary.tenantID, "/api/v1/inventory/resources", unknownID)
		if !unknown.Operational.MissingOwner || !unknown.Operational.Untagged {
			t.Fatalf("system/empty tags must not establish ownership or tagging: %+v", unknown.Operational)
		}
		for field, answer := range inventoryTestAnswers(unknown.Operational) {
			if answer.Value != domain.Unknown || answer.Source != domain.Unknown || len(answer.Candidates) != 0 {
				t.Errorf("%s should be unknown: %+v", field, answer)
			}
		}
		orphan := inventoryTestGet(t, router, primary.tenantID, "/api/v1/inventory/resources", orphanID)
		inventoryTestAssertAnswer(t, orphan.Operational.Orphan, "ORPHANED", string(domain.ProvenanceInferred), "available")
		if orphan.Operational.MissingOwner {
			t.Error("an explicit team must establish ownership even without an owner tag")
		}
	})

	t.Run("complete summary matching and pagination beyond 500", func(t *testing.T) {
		summary := inventoryTestSummary(t, router, primary.tenantID, "")
		if summary.Total != 512 || summary.UnknownOwnership != 1 || summary.Untagged != 1 {
			t.Fatalf("incorrect full summary: %+v", summary)
		}
		wantViews := map[string]int{"production": 0, "staging": 1, "development": 510, "unowned": 1, "orphaned": 1, "critical": 1, "high-cost": 510}
		if !reflect.DeepEqual(summary.Views, wantViews) {
			t.Errorf("views = %v, want %v", summary.Views, wantViews)
		}
		wantGroups := map[string]inventory.Group{
			"payments":     {Team: "payments", Application: "Legacy", Environment: "development", ResourceCount: 510, MonthlyCostEstimate: 63750},
			"storage":      {Team: "storage", Application: "Archive", Environment: "staging", ResourceCount: 1, MonthlyCostEstimate: 25},
			domain.Unknown: {Team: domain.Unknown, Application: domain.Unknown, Environment: domain.Unknown, ResourceCount: 1},
		}
		if len(summary.Groups) != len(wantGroups) {
			t.Fatalf("groups = %+v, want %v", summary.Groups, wantGroups)
		}
		for _, group := range summary.Groups {
			if want, ok := wantGroups[group.Team]; !ok || group != want {
				t.Errorf("unexpected group: %+v", group)
			}
		}
		for view, total := range wantViews {
			t.Run("view="+view, func(t *testing.T) {
				list := inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?view="+view)
				inventoryTestAssertList(t, list, total, min(total, 50), 50, 0)
			})
		}
		for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/resources"} {
			t.Run("all pages "+base, func(t *testing.T) {
				seen := map[uuid.UUID]bool{}
				for _, page := range []struct{ offset, count int }{{0, 500}, {500, 12}, {1000, 0}} {
					list := inventoryTestList(t, router, primary.tenantID, fmt.Sprintf("%s?limit=500&offset=%d", base, page.offset))
					inventoryTestAssertList(t, list, 512, page.count, 500, page.offset)
					for _, resource := range list.Resources {
						if resource.TenantID != primary.tenantID || resource.ID == deletedID || seen[resource.ID] {
							t.Fatalf("foreign, deleted, or duplicate resource in pages: %+v", resource)
						}
						seen[resource.ID] = true
						inventoryTestAssertOperational(t, resource)
					}
				}
				if len(seen) != 512 {
					t.Errorf("pagination returned only %d unique resources", len(seen))
				}
			})
		}
		for _, query := range []string{
			"team=payments", "application=Legacy", "environment=development", "view=development", "view=high-cost",
		} {
			list := inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?"+query+"&limit=500&offset=500")
			inventoryTestAssertList(t, list, 510, 10, 500, 500)
			for i, resource := range list.Resources {
				if want := fmt.Sprintf("inventory-%04d", 501+i); resource.Name != want {
					t.Errorf("%s: page name = %q, want %q", query, resource.Name, want)
				}
			}
		}
		list := inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?team=payments&application=Legacy&environment=development&limit=7&offset=503")
		inventoryTestAssertList(t, list, 510, 7, 7, 503)
		for i, resource := range list.Resources {
			if resource.Name != fmt.Sprintf("inventory-%04d", 504+i) {
				t.Errorf("filtered pagination must apply offset after matching: %+v", resource)
			}
		}
		list = inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?search=INVENTORY-0510")
		inventoryTestAssertList(t, list, 1, 1, 50, 0)
		list = inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?view=high-cost&min_monthly_cost=126")
		inventoryTestAssertList(t, list, 0, 0, 50, 0)
		if got := inventoryTestSummary(t, router, primary.tenantID, "?min_monthly_cost=25").Views["high-cost"]; got != 511 {
			t.Errorf("custom high-cost threshold count = %d, want 511", got)
		}
	})

	t.Run("invalid requests return 400 without saving rules", func(t *testing.T) {
		validBody := inventoryTestRuleBody(t, "Valid", 100, true, domain.InventoryAssignments{Team: "Payments"})
		for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/inventory/summary"} {
			for _, query := range []string{
				"view=invalid", "limit=0", "limit=-1", "limit=501", "limit=abc", "limit=", "offset=-1", "offset=abc", "offset=",
				"min_monthly_cost=NaN", "min_monthly_cost=Inf", "min_monthly_cost=-Inf", "min_monthly_cost=-1", "min_monthly_cost=abc",
			} {
				t.Run(base+"?"+query, func(t *testing.T) {
					inventoryTestRequest(t, router, primary.tenantID, http.MethodGet, base+"?"+query, "", http.StatusBadRequest)
				})
			}
		}
		invalidBodies := map[string]string{
			"bad environment":    `{"name":"Bad","tag_key":"team","tag_value":"payments","assignments":{"environment":"prod"}}`,
			"empty assignments":  `{"name":"Bad","tag_key":"team","tag_value":"payments","assignments":{}}`,
			"unknown field":      `{"name":"Bad","tag_key":"team","tag_value":"payments","assignments":{"team":"Payments"},"unexpected":true}`,
			"unknown assignment": `{"name":"Bad","tag_key":"team","tag_value":"payments","assignments":{"team":"Payments","unexpected":"value"}}`,
			"multiple objects":   validBody + ` {"name":"second"}`,
		}
		for name, body := range invalidBodies {
			t.Run(name, func(t *testing.T) {
				inventoryTestRequest(t, router, primary.tenantID, http.MethodPost, "/api/v1/inventory/rules", body, http.StatusBadRequest)
				inventoryTestRequest(t, router, primary.tenantID, http.MethodPut, "/api/v1/inventory/rules/"+uuid.New().String(), body, http.StatusBadRequest)
			})
		}
		for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/resources"} {
			inventoryTestRequest(t, router, primary.tenantID, http.MethodGet, base+"/not-a-uuid", "", http.StatusBadRequest)
		}
		inventoryTestRequest(t, router, primary.tenantID, http.MethodPut, "/api/v1/inventory/rules/not-a-uuid", validBody, http.StatusBadRequest)
		inventoryTestRequest(t, router, primary.tenantID, http.MethodDelete, "/api/v1/inventory/rules/not-a-uuid", "", http.StatusBadRequest)
		if rules := inventoryTestRules(t, router, primary.tenantID); len(rules) != 0 {
			t.Errorf("invalid requests saved rules: %+v", rules)
		}
		for _, endpoint := range []struct{ method, path, body string }{
			{http.MethodGet, "/api/v1/inventory/resources", ""},
			{http.MethodGet, "/api/v1/inventory/resources/" + primary.taggedID.String(), ""},
			{http.MethodGet, "/api/v1/inventory/summary", ""},
			{http.MethodGet, "/api/v1/inventory/rules", ""},
			{http.MethodPost, "/api/v1/inventory/rules", validBody},
			{http.MethodPut, "/api/v1/inventory/rules/" + uuid.New().String(), validBody},
			{http.MethodDelete, "/api/v1/inventory/rules/" + uuid.New().String(), ""},
		} {
			for _, header := range []string{"", "not-a-uuid"} {
				req := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body))
				if header != "" {
					req.Header.Set("X-Tenant-ID", header)
				}
				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)
				if rr.Code != http.StatusBadRequest {
					t.Errorf("%s %s with tenant %q: status = %d, want 400", endpoint.method, endpoint.path, header, rr.Code)
				}
			}
		}
	})

	t.Run("cross tenant resources and rule mutations are isolated", func(t *testing.T) {
		assignments := domain.InventoryAssignments{Team: "Payments", Application: "Billing", Environment: "production", Purpose: "Charge invoices"}
		localRule := inventoryTestCreateRule(t, pool, router, primary.tenantID, "Local mapping", 100, assignments)
		localRules := inventoryTestRules(t, router, primary.tenantID)
		foreignRules := inventoryTestRules(t, router, other.tenantID)
		if len(localRules) != 1 || localRules[0].ID != localRule.ID || localRules[0].TenantID != primary.tenantID {
			t.Fatalf("primary rule listing leaked another tenant: %+v", localRules)
		}
		if len(foreignRules) != 1 || foreignRules[0].ID != foreignRuleID || foreignRules[0].TenantID != other.tenantID {
			t.Fatalf("secondary rule listing leaked another tenant: %+v", foreignRules)
		}
		for _, scope := range []struct {
			fixture     inventorySQLFixture
			foreign     inventorySQLFixture
			foreignRule uuid.UUID
			total       int
			team        string
		}{
			{primary, other, foreignRuleID, 512, "Payments"},
			{other, primary, localRule.ID, 1, "Foreign"},
		} {
			for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/resources"} {
				list := inventoryTestList(t, router, scope.fixture.tenantID, base+"?limit=500")
				inventoryTestAssertList(t, list, scope.total, min(scope.total, 500), 500, 0)
				for _, resource := range list.Resources {
					if resource.TenantID != scope.fixture.tenantID || resource.ID == scope.foreign.taggedID {
						t.Fatalf("cross-tenant list leaked a resource: %+v", resource)
					}
				}
				resource := inventoryTestGet(t, router, scope.fixture.tenantID, base, scope.fixture.taggedID)
				inventoryTestAssertAnswer(t, resource.Operational.Team, scope.team, "OWNERSHIP_RULE", "Rule ")
				inventoryTestRequest(t, router, scope.fixture.tenantID, http.MethodGet, base+"/"+scope.foreign.taggedID.String(), "", http.StatusNotFound)
			}
			if summary := inventoryTestSummary(t, router, scope.fixture.tenantID, ""); summary.Total != scope.total {
				t.Errorf("cross-tenant summary count = %d, want %d", summary.Total, scope.total)
			}
			body := inventoryTestRuleBody(t, "Unauthorized replacement", 100, false, domain.InventoryAssignments{Team: "Intruder"})
			path := "/api/v1/inventory/rules/" + scope.foreignRule.String()
			inventoryTestRequest(t, router, scope.fixture.tenantID, http.MethodPut, path, body, http.StatusNotFound)
			inventoryTestRequest(t, router, scope.fixture.tenantID, http.MethodDelete, path, "", http.StatusNotFound)
		}
		if got := inventoryTestRules(t, router, primary.tenantID); !reflect.DeepEqual(got, localRules) {
			t.Errorf("cross-tenant requests changed the primary rule: %+v", got)
		}
		if got := inventoryTestRules(t, router, other.tenantID); !reflect.DeepEqual(got, foreignRules) {
			t.Errorf("cross-tenant requests changed the secondary rule: %+v", got)
		}
	})

	t.Run("rule roundtrip immediately remaps and restores current tags", func(t *testing.T) {
		assignments := domain.InventoryAssignments{Team: "Payments", Application: "Billing", Environment: "production", Purpose: "Charge invoices"}
		rule := inventoryTestCreateRule(t, pool, router, primary.tenantID, "Payments mapping", 100, assignments)
		if rule.TenantID != primary.tenantID || rule.TagKey != "team" || rule.TagValue != "payments" || rule.Priority != 100 || !rule.Enabled || rule.Assignments != assignments || rule.CreatedAt.IsZero() || rule.UpdatedAt.IsZero() {
			t.Fatalf("rule did not roundtrip: %+v", rule)
		}
		if rules := inventoryTestRules(t, router, primary.tenantID); len(rules) != 1 || !reflect.DeepEqual(rules[0], rule) {
			t.Fatalf("GET rules did not return the saved rule: %+v", rules)
		}
		assertMapped := func() {
			t.Helper()
			for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/resources"} {
				resource := inventoryTestGet(t, router, primary.tenantID, base, primary.taggedID)
				for field, value := range map[string]string{"team": "Payments", "application": "Billing", "environment": "production", "purpose": "Charge invoices"} {
					inventoryTestAssertAnswer(t, inventoryTestAnswers(resource.Operational)[field], value, "OWNERSHIP_RULE", rule.ID.String())
				}
				if resource.Operational.MissingOwner {
					t.Error("mapped team must clear missing_owner")
				}
				list := inventoryTestList(t, router, primary.tenantID, base+"?search=inventory-0001")
				inventoryTestAssertList(t, list, 1, 1, 50, 0)
				if !reflect.DeepEqual(list.Resources[0].Operational, resource.Operational) {
					t.Error("canonical/list snapshot did not immediately reflect rule changes")
				}
			}
			list := inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?team=Payments&application=Billing&environment=production&limit=500&offset=500")
			inventoryTestAssertList(t, list, 510, 10, 500, 500)
			summary := inventoryTestSummary(t, router, primary.tenantID, "")
			if summary.Total != 512 || summary.Views["production"] != 510 || summary.Views["development"] != 0 {
				t.Fatalf("next summary must immediately reflect all 510 rule matches: %+v", summary)
			}
			found := false
			for _, group := range summary.Groups {
				if group.Team == "Payments" && group.Application == "Billing" && group.Environment == "production" {
					found = true
					if group.ResourceCount != 510 || group.MonthlyCostEstimate != 63750 {
						t.Errorf("mapped summary group was truncated: %+v", group)
					}
				}
			}
			if !found {
				t.Error("mapped summary group is missing")
			}
		}
		assertMapped()

		// A changed provider snapshot must survive disabling/deleting a mapping;
		// reverting to historical values would hide the latest direct evidence.
		t.Cleanup(func() {
			inventoryTestSQL(t, pool, primary.tenantID, `UPDATE resource_entities SET tags=$3::jsonb WHERE tenant_id=$1 AND id=$2`, primary.tenantID, primary.taggedID, inventoryTestTags)
		})
		inventoryTestSQL(t, pool, primary.tenantID, `UPDATE resource_entities SET tags=$3::jsonb WHERE tenant_id=$1 AND id=$2`, primary.tenantID, primary.taggedID,
			`{"team":"payments","application":"Current billing","environment":"stg","purpose":"Revised direct intent","criticality":"high"}`)
		assertMapped()
		path := "/api/v1/inventory/rules/" + rule.ID.String()
		var disabled domain.OwnershipRule
		inventoryTestDecode(t, inventoryTestRequest(t, router, primary.tenantID, http.MethodPut, path, inventoryTestRuleBody(t, rule.Name, 100, false, assignments), http.StatusOK), &disabled)
		if disabled.ID != rule.ID || disabled.TenantID != primary.tenantID || disabled.Enabled || disabled.Assignments != assignments || !disabled.CreatedAt.Equal(rule.CreatedAt) {
			t.Fatalf("disabled rule did not roundtrip: %+v", disabled)
		}
		for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/resources"} {
			inventoryTestAssertDirect(t, inventoryTestGet(t, router, primary.tenantID, base, primary.taggedID), "Current billing", "staging", "Revised direct intent")
		}
		list := inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?team=Payments")
		inventoryTestAssertList(t, list, 0, 0, 50, 0)
		if rules := inventoryTestRules(t, router, primary.tenantID); len(rules) != 1 || rules[0].Enabled {
			t.Errorf("disabled rule should remain listed: %+v", rules)
		}
		inventoryTestRequest(t, router, primary.tenantID, http.MethodPut, path, inventoryTestRuleBody(t, rule.Name, 100, true, assignments), http.StatusOK)
		assertMapped()
		inventoryTestRequest(t, router, primary.tenantID, http.MethodDelete, path, "", http.StatusNoContent)
		for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/resources"} {
			inventoryTestAssertDirect(t, inventoryTestGet(t, router, primary.tenantID, base, primary.taggedID), "Current billing", "staging", "Revised direct intent")
		}
		list = inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?team=Payments")
		inventoryTestAssertList(t, list, 0, 0, 50, 0)
		if rules := inventoryTestRules(t, router, primary.tenantID); len(rules) != 0 {
			t.Errorf("deleted rule still listed: %+v", rules)
		}
		inventoryTestRequest(t, router, primary.tenantID, http.MethodPut, path, inventoryTestRuleBody(t, rule.Name, 100, true, assignments), http.StatusNotFound)
		inventoryTestRequest(t, router, primary.tenantID, http.MethodDelete, path, "", http.StatusNotFound)
	})

	t.Run("conflicting equal priority mappings remain UNKNOWN", func(t *testing.T) {
		firstAssignments := domain.InventoryAssignments{Team: "Payments", Application: "Billing", Environment: "production", Purpose: "Charge invoices"}
		secondAssignments := domain.InventoryAssignments{Team: "Risk", Application: "Fraud", Environment: "staging", Purpose: "Review charges"}
		first := inventoryTestCreateRule(t, pool, router, primary.tenantID, "Payments", 200, firstAssignments)
		second := inventoryTestCreateRule(t, pool, router, primary.tenantID, "Risk", 200, secondAssignments)
		for _, base := range []string{"/api/v1/inventory/resources", "/api/v1/resources"} {
			resource := inventoryTestGet(t, router, primary.tenantID, base, primary.taggedID)
			for field, want := range map[string][]string{
				"team": {"Payments", "Risk"}, "application": {"Billing", "Fraud"}, "environment": {"production", "staging"}, "purpose": {"Charge invoices", "Review charges"},
			} {
				answer := inventoryTestAnswers(resource.Operational)[field]
				if answer.Value != domain.Unknown || answer.Source != "CONFLICT" || answer.Confidence != 0 || !reflect.DeepEqual(answer.Candidates, want) || len(answer.Evidence) != 2 {
					t.Errorf("%s conflict was silently resolved: %+v", field, answer)
				}
				for _, id := range []uuid.UUID{first.ID, second.ID} {
					if !strings.Contains(strings.Join(answer.Evidence, "\n"), id.String()) {
						t.Errorf("%s conflict evidence omits rule %s: %+v", field, id, answer)
					}
				}
			}
			if !resource.Operational.MissingOwner {
				t.Error("conflicting team with no explicit owner must set missing_owner")
			}
		}
		list := inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?view=unowned&limit=500&offset=500")
		inventoryTestAssertList(t, list, 511, 11, 500, 500)
		list = inventoryTestList(t, router, primary.tenantID, "/api/v1/inventory/resources?team=Payments")
		inventoryTestAssertList(t, list, 0, 0, 50, 0)
		if summary := inventoryTestSummary(t, router, primary.tenantID, ""); summary.UnknownOwnership != 511 || summary.Views["production"] != 0 {
			t.Errorf("conflict summary treated ambiguous mappings as known: %+v", summary)
		}
		inventoryTestRequest(t, router, primary.tenantID, http.MethodPut, "/api/v1/inventory/rules/"+second.ID.String(), inventoryTestRuleBody(t, second.Name, 200, false, secondAssignments), http.StatusOK)
		resource := inventoryTestGet(t, router, primary.tenantID, "/api/v1/inventory/resources", primary.taggedID)
		inventoryTestAssertAnswer(t, resource.Operational.Team, "Payments", "OWNERSHIP_RULE", first.ID.String())
		inventoryTestRequest(t, router, primary.tenantID, http.MethodDelete, "/api/v1/inventory/rules/"+first.ID.String(), "", http.StatusNoContent)
		inventoryTestAssertDirect(t, inventoryTestGet(t, router, primary.tenantID, "/api/v1/inventory/resources", primary.taggedID), "Legacy", "development", "Current direct intent")
	})
}

type inventorySQLFixture struct {
	tenantID  uuid.UUID
	accountID uuid.UUID
	taggedID  uuid.UUID
}

func inventoryTestFixture(t *testing.T, pool *pgxpool.Pool, count int) inventorySQLFixture {
	t.Helper()
	fixture := inventorySQLFixture{tenantID: uuid.New(), accountID: uuid.New()}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := postgres.WithTenantTx(ctx, pool, fixture.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			for _, query := range []string{
				`DELETE FROM ownership_rules WHERE tenant_id=$1`,
				`DELETE FROM resource_entities WHERE tenant_id=$1`,
				`DELETE FROM cloud_accounts WHERE tenant_id=$1`,
				`DELETE FROM tenants WHERE id=$1`,
			} {
				if _, err := tx.Exec(ctx, query, fixture.tenantID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Errorf("cleanup inventory fixture %s: %v", fixture.tenantID, err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := postgres.WithTenantTx(ctx, pool, fixture.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Inventory integration', $2, 'active')`, fixture.tenantID, "inventory-test-"+fixture.tenantID.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO cloud_accounts (id, tenant_id, provider, account_id, name, role_arn, external_id, status)
			VALUES ($1, $2, 'aws', '123456789012', 'Disposable inventory account', 'arn:aws:iam::123456789012:role/inventory-test', $3, 'active')`, fixture.accountID, fixture.tenantID, "inventory-"+fixture.tenantID.String()); err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `INSERT INTO resource_entities
			(tenant_id, cloud_account_id, provider, account_id, region, resource_type, native_id, arn, name, status, configuration_hash, tags, observed_at, monthly_cost_estimate)
			SELECT $1, $2, 'aws', '123456789012', 'us-east-1', 'aws:ec2:instance',
			'i-inventory-' || lpad(i::text, 4, '0'),
			'arn:aws:ec2:us-east-1:123456789012:instance/i-inventory-' || lpad(i::text, 4, '0'),
			'inventory-' || lpad(i::text, 4, '0'), 'running', repeat('0', 64), $3::jsonb, NOW(), 125
			FROM generate_series(1, $4::integer) AS series(i)`, fixture.tenantID, fixture.accountID, inventoryTestTags, count)
		if err != nil {
			return err
		}
		if result.RowsAffected() != int64(count) {
			return fmt.Errorf("seeded %d resources, want %d", result.RowsAffected(), count)
		}
		return tx.QueryRow(ctx, `SELECT id FROM resource_entities WHERE tenant_id=$1 AND name='inventory-0001'`, fixture.tenantID).Scan(&fixture.taggedID)
	})
	if err != nil {
		t.Fatalf("seed inventory fixture: %v", err)
	}
	return fixture
}

func inventoryTestSeedResource(t *testing.T, pool *pgxpool.Pool, fixture inventorySQLFixture, name, resourceType, status, tags string, cost float64, deleted bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	inventoryTestSQL(t, pool, fixture.tenantID, `INSERT INTO resource_entities
		(id, tenant_id, cloud_account_id, provider, account_id, region, resource_type, native_id, arn, name, status, configuration_hash, tags, observed_at, monthly_cost_estimate, deleted_at)
		VALUES ($1, $2, $3, 'aws', '123456789012', 'us-east-1', $4, $5, $6, $7, $8, repeat('0', 64), $9::jsonb, NOW(), $10, CASE WHEN $11::boolean THEN NOW() ELSE NULL END)`,
		id, fixture.tenantID, fixture.accountID, resourceType, "inventory-"+id.String(), "arn:aws:ec2:us-east-1:123456789012:inventory/"+id.String(), name, status, tags, cost, deleted)
	return id
}

func inventoryTestSQL(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID, query string, args ...interface{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := postgres.WithTenantTx(ctx, pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, query, args...)
		return err
	})
	if err != nil {
		t.Fatalf("inventory fixture SQL: %v", err)
	}
}

func inventoryTestRequest(t *testing.T, router http.Handler, tenantID uuid.UUID, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", tenantID.String())
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != status {
		t.Fatalf("%s %s: status = %d, want %d; body: %s", method, path, rr.Code, status, rr.Body.String())
	}
	if status == http.StatusNoContent && rr.Body.Len() != 0 {
		t.Errorf("DELETE must not return a response body: %s", rr.Body.String())
	}
	return rr
}

func inventoryTestDecode(t *testing.T, rr *httptest.ResponseRecorder, result interface{}) {
	t.Helper()
	if !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected JSON content type, got %q", rr.Header().Get("Content-Type"))
	}
	if err := json.Unmarshal(rr.Body.Bytes(), result); err != nil {
		t.Fatalf("decode inventory response: %v; body: %s", err, rr.Body.String())
	}
}

func inventoryTestGet(t *testing.T, router http.Handler, tenantID uuid.UUID, base string, id uuid.UUID) domain.ResourceEntity {
	t.Helper()
	rr := inventoryTestRequest(t, router, tenantID, http.MethodGet, base+"/"+id.String(), "", http.StatusOK)
	var resource domain.ResourceEntity
	inventoryTestDecode(t, rr, &resource)
	if resource.ID != id || resource.TenantID != tenantID {
		t.Fatalf("wrong resource identity: %+v", resource)
	}
	inventoryTestAssertOperational(t, resource)
	var wire struct {
		Operational map[string]json.RawMessage `json:"operational"`
	}
	inventoryTestDecode(t, rr, &wire)
	var missingOwner interface{}
	if err := json.Unmarshal(wire.Operational["missing_owner"], &missingOwner); err != nil {
		t.Fatalf("missing_owner must be included in operational JSON: %v", err)
	}
	if _, ok := missingOwner.(bool); !ok {
		t.Fatalf("missing_owner must be a JSON bool, got %v", missingOwner)
	}
	return resource
}

func inventoryTestList(t *testing.T, router http.Handler, tenantID uuid.UUID, path string) inventory.ListResult {
	t.Helper()
	var result inventory.ListResult
	inventoryTestDecode(t, inventoryTestRequest(t, router, tenantID, http.MethodGet, path, "", http.StatusOK), &result)
	if result.Resources == nil {
		t.Fatal("resource lists must use an array, including when empty")
	}
	return result
}

func inventoryTestAssertList(t *testing.T, result inventory.ListResult, total, count, limit, offset int) {
	t.Helper()
	if result.Total != total || len(result.Resources) != count || result.Limit != limit || result.Offset != offset {
		t.Fatalf("list total/count/limit/offset = %d/%d/%d/%d, want %d/%d/%d/%d", result.Total, len(result.Resources), result.Limit, result.Offset, total, count, limit, offset)
	}
}

func inventoryTestSummary(t *testing.T, router http.Handler, tenantID uuid.UUID, query string) inventory.Summary {
	t.Helper()
	var result inventory.Summary
	inventoryTestDecode(t, inventoryTestRequest(t, router, tenantID, http.MethodGet, "/api/v1/inventory/summary"+query, "", http.StatusOK), &result)
	return result
}

func inventoryTestRules(t *testing.T, router http.Handler, tenantID uuid.UUID) []domain.OwnershipRule {
	t.Helper()
	var result struct {
		Rules []domain.OwnershipRule `json:"rules"`
	}
	inventoryTestDecode(t, inventoryTestRequest(t, router, tenantID, http.MethodGet, "/api/v1/inventory/rules", "", http.StatusOK), &result)
	if result.Rules == nil {
		t.Fatal("rule lists must use an array, including when empty")
	}
	return result.Rules
}

func inventoryTestRuleBody(t *testing.T, name string, priority int, enabled bool, assignments domain.InventoryAssignments) string {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"name": name, "tag_key": "team", "tag_value": "payments", "priority": priority, "enabled": enabled, "assignments": assignments,
	})
	if err != nil {
		t.Fatalf("marshal inventory rule: %v", err)
	}
	return string(body)
}

func inventoryTestCreateRule(t *testing.T, pool *pgxpool.Pool, router http.Handler, tenantID uuid.UUID, name string, priority int, assignments domain.InventoryAssignments) domain.OwnershipRule {
	t.Helper()
	var rule domain.OwnershipRule
	inventoryTestDecode(t, inventoryTestRequest(t, router, tenantID, http.MethodPost, "/api/v1/inventory/rules", inventoryTestRuleBody(t, name, priority, true, assignments), http.StatusCreated), &rule)
	if rule.ID == uuid.Nil {
		t.Fatal("created rule must have an ID")
	}
	// Subtest-local cleanup prevents a failed lifecycle from contaminating later assertions.
	t.Cleanup(func() {
		inventoryTestSQL(t, pool, tenantID, `DELETE FROM ownership_rules WHERE tenant_id=$1 AND id=$2`, tenantID, rule.ID)
	})
	return rule
}

func inventoryTestAnswers(op *domain.OperationalInventory) map[string]domain.InventoryAnswer {
	return map[string]domain.InventoryAnswer{
		"owner": op.Owner, "team": op.Team, "application": op.Application, "environment": op.Environment,
		"purpose": op.Purpose, "criticality": op.Criticality, "orphan": op.Orphan,
	}
}

func inventoryTestAssertOperational(t *testing.T, resource domain.ResourceEntity) {
	t.Helper()
	if resource.Operational == nil {
		t.Fatalf("resource %s omits operational", resource.ID)
	}
	for field, answer := range inventoryTestAnswers(resource.Operational) {
		if answer.Value == "" || answer.Source == "" || len(answer.Evidence) == 0 || answer.Candidates == nil {
			t.Errorf("resource %s %s omits value/source/evidence/candidates: %+v", resource.ID, field, answer)
		}
	}
}

func inventoryTestAssertAnswer(t *testing.T, answer domain.InventoryAnswer, value, source, evidence string) {
	t.Helper()
	if answer.Value != value || answer.Source != source || len(answer.Candidates) != 0 || !strings.Contains(strings.Join(answer.Evidence, "\n"), evidence) {
		t.Errorf("answer = %+v, want value %q, source %q, evidence containing %q, and no candidates", answer, value, source, evidence)
	}
}

func inventoryTestAssertDirect(t *testing.T, resource domain.ResourceEntity, application, environment, purpose string) {
	t.Helper()
	inventoryTestAssertOperational(t, resource)
	for field, value := range map[string]string{"team": "payments", "application": application, "environment": environment, "purpose": purpose} {
		inventoryTestAssertAnswer(t, inventoryTestAnswers(resource.Operational)[field], value, string(domain.ProvenanceObservedTag), "tag:")
	}
	inventoryTestAssertAnswer(t, resource.Operational.Owner, domain.Unknown, domain.Unknown, "No explicit")
	if resource.Operational.MissingOwner {
		t.Error("a direct team tag must clear missing_owner")
	}
}
