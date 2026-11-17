package postgres

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrations(t *testing.T) {
	upFiles, err := LoadEmbeddedMigrations("up")
	if err != nil {
		t.Fatalf("unexpected error loading up migrations: %v", err)
	}

	if len(upFiles) == 0 {
		t.Fatal("expected at least 1 up migration file embedded")
	}

	firstUp := upFiles[0]
	if firstUp.Version != "000001" {
		t.Errorf("expected version 000001, got %s", firstUp.Version)
	}

	// Invariants check: Must enable Row-Level Security
	if !strings.Contains(firstUp.Content, "ENABLE ROW LEVEL SECURITY") {
		t.Errorf("migration %s must enable ROW LEVEL SECURITY", firstUp.Filename)
	}

	// Must create core tables
	requiredTables := []string{
		"tenants",
		"cloud_accounts",
		"resource_entities",
		"resource_relationships",
		"ownership_records",
		"action_plans",
		"audit_logs",
	}
	for _, tbl := range requiredTables {
		if !strings.Contains(firstUp.Content, tbl) {
			t.Errorf("migration %s must contain table definition for '%s'", firstUp.Filename, tbl)
		}
	}

	// Check down migration exists
	downFiles, err := LoadEmbeddedMigrations("down")
	if err != nil {
		t.Fatalf("unexpected error loading down migrations: %v", err)
	}
	if len(downFiles) == 0 {
		t.Fatal("expected at least 1 down migration file embedded")
	}
	if !strings.Contains(downFiles[0].Content, "DROP TABLE") {
		t.Errorf("down migration must drop tables, got: %s", downFiles[0].Content)
	}
}
