# Cloud Cost Intelligence & Explanation Engine Matrix

## Executive Overview

Cradle Phase 4 builds deep cloud cost intelligence. Rather than simply displaying AWS billing tables, Cradle correlates financial spend directly with discovered infrastructure state and operational context (team, application, environment).

The platform answers the central question engineering and leadership teams ask:
> **"Why did our bill increase?"**

Every cost figure returned by Cradle includes strict epistemological provenance and time period boundaries:
- **`OBSERVED_FROM_PROVIDER`**: Direct AWS Cost Explorer unblended spend.
- **`DERIVED_BY_CRADLE`**: Normalized cross-resource aggregations and operational attribution.
- **`INFERRED_BY_CRADLE`**: Statistical anomaly classifications and forward-looking forecasts with confidence intervals.

---

## 1. Dimensional Cost Breakdown Matrix

Cradle aggregates spend across 7 primary operational and architectural dimensions:

| Dimension | Aggregation Key | Attribution Mechanism | Provenance |
| :--- | :--- | :--- | :--- |
| **Account** | `cloud_account_id` | Mapped to registered AWS Account ID and friendly name | `AWS Cost Explorer (Observed)` |
| **Region** | `region` / `dimensions->>'region'` | Geographic AWS region (e.g., `us-east-1`, `eu-west-1`, `global`) | `AWS Cost Explorer (Observed)` |
| **Service** | `service_name` | Canonical AWS service name (e.g., `Amazon Elastic Kubernetes Service`) | `AWS Cost Explorer (Observed)` |
| **Resource** | `resource_arn` / `resource_id` | Associated `resource_entities` record and resource-level billing tags | `AWS Cost Explorer (Observed)` |
| **Team** | `dimensions->>'team'` or resource `team` tag | Attributed to responsible engineering team (or `UNKNOWN` if missing) | `Derived from Operational Inventory` |
| **Application** | `dimensions->>'application'` or resource `app` tag | Attributed to microservice / application workload (or `UNKNOWN`) | `Derived from Operational Inventory` |
| **Environment** | `dimensions->>'environment'` or resource `env` tag | Attributed to `production`, `staging`, `development` (or `UNKNOWN`) | `Derived from Operational Inventory` |

---

## 2. Cost Explanation Engine Architecture

When analyzing spend changes between two periods (e.g. Current 30-day window vs. Prior 30-day window):

```
┌────────────────────────────────────────────────────────────────────────┐
│                        Cost Explanation Engine                         │
│                                                                        │
│   Previous Period ($7,920) ──────────────► Current Period ($10,090)    │
│                        Delta: +$2,170 (+27.4%)                         │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 1. Isolate Primary Service Cost Drivers                                │
│    - Amazon Elastic Kubernetes Service:  +$1,240.00 (+68.9%)           │
│    - Amazon Relational Database Service: +$620.00 (+29.5%)             │
│    - Amazon Virtual Private Cloud (NAT): +$310.00 (+68.9%)             │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 2. Correlate with Discovered Infrastructure History                    │
│    - EKS: Provisioned cluster 'prod-payments-eks' and 3 worker nodes   │
│    - RDS: Scaled DB cluster 'orders-aurora' from db.r5.large to 2xlarge│
│    - NAT: High-throughput outbound traffic surge in us-east-1a         │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 3. Operational Attribution & Actionable Recommendations                │
│    - Team Attribution: Payments (+$1,550), Core-Infra (+$620)          │
│    - Waste Correlation: $310 correlates with unattached EBS & idle NAT │
│    - Executive Headline: "Monthly spend increased 27.4% (+$2,170.00)..."│
└────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Statistical Cost Anomaly Detection

Anomalies are detected using trailing moving average baselines:

1. **Daily Aggregation**: Daily spend is tracked across each service.
2. **Trailing Baseline**: Baseline $B$ is calculated as the mean of the prior 7 days:
   $$B = \frac{1}{7} \sum_{i=1}^7 S_{d-i}$$
3. **Surge Threshold**: A spend event is flagged as anomalous when:
   $$\text{Actual} > 1.25 \times B \quad \text{and} \quad (\text{Actual} - B) > \$10.00$$
4. **Severity Classification**:
   - `CRITICAL`: Surge $> 100\%$ or deviation $> \$200.00$
   - `HIGH`: Surge $> 50\%$ or deviation $> \$100.00$
   - `MEDIUM`: Surge $> 25\%$ or deviation $> \$10.00$

---

## 4. Forecasting & Run-Rate Methodology

1. **Estimated Monthly Spend (Run-Rate)**:
   $$\text{Run-Rate} = \text{MTD Spend} + \left( \text{Daily Run-Rate} \times (\text{Days In Month} - \text{Current Day}) \right)$$
2. **Period Forecast**:
   - Forecasts spend across 30, 60, or 90 days using linear trend slope combined with moving average.
   - **80% Confidence Interval**:
     $$\text{Forecast} \pm 1.28 \times \sigma \times \sqrt{1 + \frac{d}{30}}$$
     where $\sigma$ is standard deviation and $d$ is the forecast day offset.

---

## 5. API Endpoints

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/cost` | `GET` | Interactive Cost Intelligence & Explanation Engine Web Dashboard |
| `/api/v1/cost/summary` | `GET` | High-level summary of total spend and service breakdown over $N$ days |
| `/api/v1/cost/breakdown` | `GET` | Dimensional breakdown (`service`, `account`, `region`, `team`, `app`, `env`, `resource`) |
| `/api/v1/cost/trends` | `GET` | Daily or monthly spend trends with 7-day rolling moving averages |
| `/api/v1/cost/forecast` | `GET` | Forward-looking spend projections with 80% confidence interval bands |
| `/api/v1/cost/anomalies` | `GET` | Statistical spend surges exceeding trailing baselines |
| `/api/v1/cost/explain` | `GET` / `POST` | Natural-language explanation of cost changes with correlated infrastructure changes |
