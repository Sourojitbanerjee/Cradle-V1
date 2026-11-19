# Operational Inventory & Ownership Derivation Matrix

## Overview

The Cradle Operational Inventory transforms raw cloud infrastructure discoveries into an actionable, accountability-focused operational inventory.

Every infrastructure resource in Cradle is evaluated to answer four fundamental questions:

| Question | Operational Field | Primary Sources | Epistemological Rule |
| :--- | :--- | :--- | :--- |
| **WHO owns this?** | `Owner` & `Team` | Verified ownership records, ownership rules (`tag_key` = `tag_value`), user resource tags (`owner`, `team`, `owned-by`, etc.) | Never fabricate an owner. If unassigned, status is `UNKNOWN` and flagged in the Unknown Ownership queue. |
| **WHAT application does it belong to?** | `Application` | User tags (`application`, `app`, `service`), matching ownership rules | If no application tag or rule matches, status is `UNKNOWN`. |
| **WHAT environment is it?** | `Environment` | User tags (`environment`, `env`, `stage`), matching ownership rules | Normalized to `production`, `staging`, `development`. Other/missing values remain `UNKNOWN`. |
| **WHY does it exist?** | `Purpose` | User tags (`purpose`, `business-purpose`, `description`), matching ownership rules | Direct intent evidence required; otherwise `UNKNOWN`. |

Additionally, resources are classified by **Criticality** (`critical`, `high`, `medium`, `low`) and **Orphan Status** (`ORPHANED`, `NOT_ORPHANED`, `UNKNOWN`).

---

## Precedence & Resolution Engine

When multiple inputs provide values for an operational field, the resolver (`pkg/inventory/resolver.go`) applies strict, deterministic precedence:

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Verified Manual Ownership Records (Precedence: 4)        │
│    Verified by human or explicit audit trail                │
├─────────────────────────────────────────────────────────────┤
│ 2. Unverified Manual Ownership Records (Precedence: 3)      │
│    Explicit human entry awaiting verification               │
├─────────────────────────────────────────────────────────────┤
│ 3. Tenant Ownership Rules (Precedence: 2)                  │
│    Configured tag matches; higher Priority wins (0-10000)   │
├─────────────────────────────────────────────────────────────┤
│ 4. Observed Provider Resource Tags (Precedence: 1)         │
│    Direct tags parsed from cloud provider APIs              │
├─────────────────────────────────────────────────────────────┤
│ 5. Default Fallback                                         │
│    Value = "UNKNOWN", Source = "UNKNOWN", Confidence = 0.0  │
└─────────────────────────────────────────────────────────────┘
```

> **Epistemological Constraint:** Unverified historical inferences (`OwnershipSourceTagInferred`) do **not** override current evidence.

### Conflicting Values
If multiple candidates tie at the highest precedence and priority with distinct values:
- `Value` is set to `UNKNOWN`
- `Source` is set to `CONFLICT`
- `Confidence` is set to `0.0`
- `Candidates` lists all competing candidate values sorted deterministically.

---

## Tag Mapping & Normalization

### Standard Tag Keys
The following case-insensitive tag keys are automatically mapped to operational fields:

| Field | Recognized Tag Keys | Normalization |
| :--- | :--- | :--- |
| `owner` | `owner`, `owned-by`, `owner_email` | Trimmed non-empty string |
| `team` | `team`, `team-name`, `team_name` | Trimmed non-empty string |
| `application` | `application`, `app`, `service` | Trimmed non-empty string |
| `environment` | `environment`, `env`, `stage` | `prod`/`production` → `production`<br>`stage`/`staging`/`stg` → `staging`<br>`dev`/`development` → `development`<br>Other values → `UNKNOWN` |
| `purpose` | `purpose`, `business-purpose`, `description` | Trimmed non-empty string |
| `criticality` | `criticality`, `business-criticality` | `critical`, `high`, `medium`, `low` (case-insensitive) |

Tags starting with system prefixes (e.g. `aws:`) and empty tag keys/values are excluded from user tag counts.

---

## Orphan Detection

Orphan status is determined strictly by explicit cloud provider evidence:

| Resource Type | Evidence Condition | Result | Provenance |
| :--- | :--- | :--- | :--- |
| `aws:ec2:volume` | `Status == "available"` (unattached) | `ORPHANED` | `INFERRED_BY_CRADLE` |
| `aws:ec2:volume` | `Status == "in_use"` (attached) | `NOT_ORPHANED` | `INFERRED_BY_CRADLE` |
| `aws:ec2:eip` | `instance_id == ""` and `network_interface_id == ""` | `ORPHANED` | `INFERRED_BY_CRADLE` |
| `aws:ec2:eip` | `instance_id != ""` or `network_interface_id != ""` | `NOT_ORPHANED` | `INFERRED_BY_CRADLE` |
| All others | Provider state does not supply sufficient detachment facts | `UNKNOWN` | `UNKNOWN` |

> Missing relationship graph edges alone are **insufficient** to declare an orphan; positive provider state evidence is required.

---

## Inventory Views & Queues

The platform exposes dedicated views via `/api/v1/inventory/resources?view=<view>` and the web UI at `/inventory`:

1. **`production`**: Resources with `Operational.Environment.Value == "production"`.
2. **`staging`**: Resources with `Operational.Environment.Value == "staging"`.
3. **`development`**: Resources with `Operational.Environment.Value == "development"`.
4. **`unowned` ("Unknown Ownership" Queue)**: Resources where both `Owner` and `Team` are `UNKNOWN` (`Operational.MissingOwner == true`).
5. **`orphaned`**: Resources verified as orphaned (`Operational.Orphan.Value == "ORPHANED"`).
6. **`critical`**: Resources with `Operational.Criticality.Value == "critical"`.
7. **`high-cost`**: Resources whose monthly cost estimate meets or exceeds the threshold (default: $100/mo). Zero or unknown estimates are excluded.

---

## User-Defined Ownership Rules

Tenants can configure deterministic classification rules matching provider tags:

- **Matcher**: Exact match on `tag_key` and `tag_value` (e.g., `team = payments`).
- **Priority**: Integer from `0` to `10000` (higher numbers take precedence).
- **Assignments**: Any combination of `owner`, `team`, `application`, `environment`, `purpose`, and `criticality`.
- **Database Isolation**: Stored in `ownership_rules` table with PostgreSQL Row-Level Security (`tenant_id = app.current_tenant_id`).
