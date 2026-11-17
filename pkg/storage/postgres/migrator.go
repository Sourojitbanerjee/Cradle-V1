package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrator manages embedded SQL schema migrations against PostgreSQL.
type Migrator struct {
	pool *pgxpool.Pool
}

// NewMigrator returns a new Migrator instance.
func NewMigrator(pool *pgxpool.Pool) *Migrator {
	return &Migrator{pool: pool}
}

// MigrationFile encapsulates a parsed migration file.
type MigrationFile struct {
	Version   string
	Direction string // "up" or "down"
	Filename  string
	Content   string
}

// Up runs all pending up migrations sequentially within individual transactions.
func (m *Migrator) Up(ctx context.Context) error {
	if m.pool == nil {
		return errors.New("cannot run migrations: database connection pool is nil")
	}

	// 1. Ensure migrations tracking table exists
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure schema_migrations table: %w", err)
	}

	// 2. Load and sort up migration files
	files, err := m.loadMigrationFiles("up")
	if err != nil {
		return fmt.Errorf("failed to load migration files: %w", err)
	}

	// 3. Apply pending migrations
	for _, file := range files {
		applied, err := m.isMigrationApplied(ctx, file.Version)
		if err != nil {
			return fmt.Errorf("failed to check status for migration %s: %w", file.Version, err)
		}
		if applied {
			continue
		}

		if err := m.applyMigration(ctx, file); err != nil {
			return fmt.Errorf("failed executing migration %s (%s): %w", file.Version, file.Filename, err)
		}
	}

	return nil
}

func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(64) PRIMARY KEY,
			filename VARCHAR(255) NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	_, err := m.pool.Exec(ctx, query)
	return err
}

func (m *Migrator) isMigrationApplied(ctx context.Context, version string) (bool, error) {
	var count int
	query := `SELECT COUNT(1) FROM schema_migrations WHERE version = $1`
	err := m.pool.QueryRow(ctx, query, version).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *Migrator) applyMigration(ctx context.Context, file MigrationFile) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Execute migration content
	if _, err := tx.Exec(ctx, file.Content); err != nil {
		return err
	}

	// Record applied migration
	recordQuery := `INSERT INTO schema_migrations (version, filename) VALUES ($1, $2)`
	if _, err := tx.Exec(ctx, recordQuery, file.Version, file.Filename); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (m *Migrator) loadMigrationFiles(direction string) ([]MigrationFile, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}

	suffix := fmt.Sprintf(".%s.sql", direction)
	var files []MigrationFile

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}

		parts := strings.Split(entry.Name(), "_")
		if len(parts) == 0 {
			continue
		}
		version := parts[0]

		contentBytes, err := fs.ReadFile(migrationsFS, "migrations/"+entry.Name())
		if err != nil {
			return nil, err
		}

		files = append(files, MigrationFile{
			Version:   version,
			Direction: direction,
			Filename:  entry.Name(),
			Content:   string(contentBytes),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Version < files[j].Version
	})

	return files, nil
}

// LoadEmbeddedMigrations returns parsed migration files for inspection and unit testing.
func LoadEmbeddedMigrations(direction string) ([]MigrationFile, error) {
	m := &Migrator{}
	return m.loadMigrationFiles(direction)
}
