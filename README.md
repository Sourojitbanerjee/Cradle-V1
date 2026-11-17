# Cradle — Cloud Infrastructure Control Platform

> An out-of-band cloud infrastructure control plane that makes cloud infrastructure understandable, observable, governable, cost-efficient, secure, and safely automated.

---

## The 5 Core Questions

For every cloud resource in your infrastructure, Cradle answers:
1. **WHAT is this?** (Canonical identity, type, configuration, state)
2. **WHY does it exist?** (Business purpose, parent workload, application service)
3. **WHO owns it?** (Team, engineer, Slack channel, PagerDuty schedule, repository)
4. **HOW MUCH does it cost?** (Real-time run-rate, historical trend, waste factor)
5. **WHAT happens if we change or delete it?** (Blast radius, downstream dependencies, network connectivity, data loss risk)

---

## Architectural Principles

- **Observe First**: Evolve systematically: `OBSERVE` → `UNDERSTAND` → `PREDICT` → `RECOMMEND` → `APPROVE` → `ACT` → `VERIFY`.
- **Zero-Trust & Least Privilege**: Never require long-lived AWS secret access keys; all integration relies on ephemeral `sts:AssumeRole` with unique cryptographic `ExternalId`.
- **Mathematical Multi-Tenancy**: Enforced via PostgreSQL 16 **Row-Level Security (RLS)** at the database engine layer.
- **Append-Only Tamper-Evident Audit Ledger**: Every administrative and infrastructure action is cryptographically chained using HMAC-SHA256.
- **Credential Redaction in Logs**: Zero secrets exposed in logs, memory dumps, or API traces.

---

## Repository Structure

```text
.
├── apps/
│   ├── api/                    # Control Plane REST API Server (Go / Chi)
│   │   ├── cmd/server/         # Entrypoint & HTTP routing
│   │   └── internal/           # Handlers & Middlewares (Tenant, Structured Logging)
│   └── worker/                 # Background Discovery & Ingestion Worker
│       └── cmd/worker/         # Entrypoint & Task Engine
├── pkg/
│   ├── domain/                 # Canonical Entity Models & Contracts
│   ├── crypto/                 # ExternalId generator & HMAC-SHA256 Audit Chaining
│   ├── storage/                # PostgreSQL Connection Pool & Embedded RLS Migrations
│   └── telemetry/              # Structured Slog Logger with Secret Redactor
├── deploy/
│   └── docker-compose.yml      # Local PostgreSQL 16 (pgvector) & Redis 7
├── Makefile                    # Developer workflows (test, lint, build, run)
└── go.mod
```

---

## Getting Started

### Prerequisites
- Go 1.25+
- Docker & Docker Compose (optional for local PostgreSQL and Redis)

### Developer Commands

```bash
# Run static analysis and linting
make lint

# Run all unit and integration tests (with race detection)
make test

# Build compiled production binaries
make build

# Start local PostgreSQL and Redis
make docker-up

# Run the API server
make run-api

# Run the Discovery Worker
make run-worker
```
