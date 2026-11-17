# AWS Least-Privilege IAM Integration Guide

> **Security Mandate**: Cradle operates strictly as a **Read-Only Out-of-Band Control Plane**.
> Cradle **never** requires, accepts, or stores long-lived AWS IAM Access Keys (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`). All access is granted via cross-account IAM role assumption using ephemeral AWS Security Token Service (STS) credentials protected by a unique cryptographic `ExternalId`.

---

## 1. Why Read-Only?

Phase 1 discovery is strictly non-destructive. Cradle inspects infrastructure to build an accurate, continuous, and observable inventory model without modifying your running resources.

- **Zero Write Permissions**: Cradle cannot launch instances, terminate databases, modify security group rules, or alter S3 bucket policies.
- **Data Protection**: Cradle does not read the data stored *inside* your S3 objects, DynamoDB tables, or RDS databases. It only reads the infrastructure *metadata* (e.g. S3 bucket encryption settings, public access blocks, RDS instance classes, VPC subnet routing).
- **Ephemeral Credentials**: Temporary STS credentials expire automatically within 60 minutes and exist only in Cradle's volatile memory.

---

## 2. Required IAM Permissions & Rationale

| Service | Action | Why Cradle Needs This (Discovery Rationale) |
| :--- | :--- | :--- |
| **STS** | `sts:GetCallerIdentity` | Verifies account ID and connectivity health during account registration. |
| **EC2** | `ec2:DescribeInstances`<br/>`ec2:DescribeInstanceStatus` | Discovers virtual servers, compute sizing, state (running/stopped), VPC/Subnet associations, and tags. |
| **EBS** | `ec2:DescribeVolumes`<br/>`ec2:DescribeSnapshots` | Discovers block storage volumes, attachment state, encryption status, and snapshot age/lineage. Snapshots are filtered to customer-owned (`self`). |
| **VPC & Networking** | `ec2:DescribeVpcs`<br/>`ec2:DescribeSubnets`<br/>`ec2:DescribeRouteTables`<br/>`ec2:DescribeInternetGateways`<br/>`ec2:DescribeNatGateways`<br/>`ec2:DescribeAddresses` | Maps network topology, subnet boundaries, public routing pathways, NAT gateway bandwidth sinks, and allocated Elastic IPs (EIPs). |
| **Security Groups** | `ec2:DescribeSecurityGroups`<br/>`ec2:DescribeSecurityGroupRules` | Analyzes network security perimeters and ingress/egress rules to build traffic reachability graphs. |
| **S3** | `s3:ListAllMyBuckets`<br/>`s3:GetBucketLocation`<br/>`s3:GetBucketTagging`<br/>`s3:GetBucketEncryption`<br/>`s3:GetBucketPolicy`<br/>`s3:GetBucketPublicAccessBlock` | Discovers storage buckets and audits posture against public exposure and missing encryption. *Does NOT read bucket object contents (`s3:GetObject` is strictly omitted).* |
| **RDS** | `rds:DescribeDBInstances`<br/>`rds:DescribeDBClusters`<br/>`rds:ListTagsForResource` | Discovers relational database instances, Multi-AZ topology, engine versions, and storage allocations. |
| **Load Balancing** | `elasticloadbalancing:DescribeLoadBalancers`<br/>`elasticloadbalancing:DescribeTargetGroups` | Maps ingress traffic entrypoints (ALB/NLB) and their downstream target group routing to compute workloads. |
| **Lambda** | `lambda:ListFunctions`<br/>`lambda:ListTags` | Catalogs serverless compute functions, runtimes, memory configurations, and VPC attachments. |
| **ECS** | `ecs:ListClusters`<br/>`ecs:DescribeClusters`<br/>`ecs:ListServices`<br/>`ecs:DescribeServices` | Discovers containerized ECS workloads, service replica counts, and task definitions. |
| **EKS** | `eks:ListClusters`<br/>`eks:DescribeCluster`<br/>`eks:ListNodegroups`<br/>`eks:DescribeNodegroup` | Discovers managed Kubernetes clusters, control plane status, and worker node group configurations. |
| **CloudWatch** | `cloudwatch:GetMetricStatistics`<br/>`cloudwatch:GetMetricData` | Collects sliding-window resource utilization metrics (CPU, memory, network, connections) for waste and idle detection. |
| **Cost Explorer** | `ce:GetCostAndUsage`<br/>`ce:GetDimensionValues` | Ingests unblended spend and usage quantities per service and usage type for cost intelligence. |

---

## 3. The Confused Deputy Protection (`sts:ExternalId`)

To prevent the **Confused Deputy Problem** (where a malicious party guesses your AWS Account ID and connects your account to their Cradle tenant), the trust policy requires a cryptographically random `sts:ExternalId` issued specifically to your organization:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::CRADLE_AWS_ACCOUNT_ID:root"
      },
      "Action": "sts:AssumeRole",
      "Condition": {
        "StringEquals": {
          "sts:ExternalId": "YOUR_UNIQUE_CRADLE_EXTERNAL_ID"
        }
      }
    }
  ]
}
```

---

## 4. Deployment Option A: AWS CloudFormation Template

Deploy this template in your AWS management or member account:

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: 'Cradle Control Plane - Read-Only Discovery Cross-Account IAM Role'

Parameters:
  CradleAccountId:
    Type: String
    Description: 'The AWS Account ID hosting the Cradle Control Plane'
    AllowedPattern: '^[0-9]{12}$'
  CradleExternalId:
    Type: String
    Description: 'Unique External ID generated in your Cradle console'
    MinLength: 32
    MaxLength: 128

Resources:
  CradleDiscoveryRole:
    Type: AWS::IAM::Role
    Properties:
      RoleName: CradleControlPlaneReadOnlyRole
      Description: 'Allows Cradle Control Plane to safely discover and monitor cloud topology'
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal:
              AWS: !Sub 'arn:aws:iam::${CradleAccountId}:root'
            Action: 'sts:AssumeRole'
            Condition:
              StringEquals:
                'sts:ExternalId': !Ref CradleExternalId
      Policies:
        - PolicyName: CradleReadOnlyDiscoveryPolicy
          PolicyDocument:
            Version: '2012-10-17'
            Statement:
              - Sid: Ec2NetworkingDiscovery
                Effect: Allow
                Action:
                  - 'ec2:DescribeInstances'
                  - 'ec2:DescribeInstanceStatus'
                  - 'ec2:DescribeVolumes'
                  - 'ec2:DescribeSnapshots'
                  - 'ec2:DescribeVpcs'
                  - 'ec2:DescribeSubnets'
                  - 'ec2:DescribeRouteTables'
                  - 'ec2:DescribeInternetGateways'
                  - 'ec2:DescribeNatGateways'
                  - 'ec2:DescribeAddresses'
                  - 'ec2:DescribeSecurityGroups'
                  - 'ec2:DescribeSecurityGroupRules'
                  - 'ec2:DescribeRegions'
                  - 'ec2:DescribeAvailabilityZones'
                Resource: '*'
              - Sid: S3Discovery
                Effect: Allow
                Action:
                  - 's3:ListAllMyBuckets'
                  - 's3:GetBucketLocation'
                  - 's3:GetBucketTagging'
                  - 's3:GetBucketEncryption'
                  - 's3:GetBucketPolicy'
                  - 's3:GetBucketPublicAccessBlock'
                Resource: '*'
              - Sid: RdsDiscovery
                Effect: Allow
                Action:
                  - 'rds:DescribeDBInstances'
                  - 'rds:DescribeDBClusters'
                  - 'rds:ListTagsForResource'
                Resource: '*'
              - Sid: ElbDiscovery
                Effect: Allow
                Action:
                  - 'elasticloadbalancing:DescribeLoadBalancers'
                  - 'elasticloadbalancing:DescribeTargetGroups'
                  - 'elasticloadbalancing:DescribeTags'
                Resource: '*'
              - Sid: ServerlessAndContainersDiscovery
                Effect: Allow
                Action:
                  - 'lambda:ListFunctions'
                  - 'lambda:ListTags'
                  - 'ecs:ListClusters'
                  - 'ecs:DescribeClusters'
                  - 'ecs:ListServices'
                  - 'ecs:DescribeServices'
                  - 'eks:ListClusters'
                  - 'eks:DescribeCluster'
                  - 'eks:ListNodegroups'
                  - 'eks:DescribeNodegroup'
                Resource: '*'
              - Sid: CostAndMetricsDiscovery
                Effect: Allow
                Action:
                  - 'cloudwatch:GetMetricStatistics'
                  - 'cloudwatch:GetMetricData'
                  - 'cloudwatch:ListMetrics'
                  - 'ce:GetCostAndUsage'
                  - 'ce:GetDimensionValues'
                Resource: '*'

Outputs:
  RoleArn:
    Description: 'IAM Role ARN to configure in Cradle Console'
    Value: !GetAtt CradleDiscoveryRole.Arn
```

---

## 5. Deployment Option B: Terraform / OpenTofu Module

```hcl
variable "cradle_aws_account_id" {
  type        = string
  description = "The AWS Account ID hosting the Cradle Control Plane"
}

variable "cradle_external_id" {
  type        = string
  description = "Unique External ID provided by Cradle for your tenant"
}

data "aws_iam_policy_document" "cradle_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${var.cradle_aws_account_id}:root"]
    }

    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [var.cradle_external_id]
    }
  }
}

data "aws_iam_policy_document" "cradle_read_only" {
  statement {
    sid    = "Ec2NetworkingDiscovery"
    effect = "Allow"
    actions = [
      "ec2:DescribeInstances",
      "ec2:DescribeInstanceStatus",
      "ec2:DescribeVolumes",
      "ec2:DescribeSnapshots",
      "ec2:DescribeVpcs",
      "ec2:DescribeSubnets",
      "ec2:DescribeRouteTables",
      "ec2:DescribeInternetGateways",
      "ec2:DescribeNatGateways",
      "ec2:DescribeAddresses",
      "ec2:DescribeSecurityGroups",
      "ec2:DescribeSecurityGroupRules",
      "ec2:DescribeRegions",
      "ec2:DescribeAvailabilityZones",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "S3Discovery"
    effect = "Allow"
    actions = [
      "s3:ListAllMyBuckets",
      "s3:GetBucketLocation",
      "s3:GetBucketTagging",
      "s3:GetBucketEncryption",
      "s3:GetBucketPolicy",
      "s3:GetBucketPublicAccessBlock",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "RdsDiscovery"
    effect = "Allow"
    actions = [
      "rds:DescribeDBInstances",
      "rds:DescribeDBClusters",
      "rds:ListTagsForResource",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "ElbDiscovery"
    effect = "Allow"
    actions = [
      "elasticloadbalancing:DescribeLoadBalancers",
      "elasticloadbalancing:DescribeTargetGroups",
      "elasticloadbalancing:DescribeTags",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "ContainersServerlessDiscovery"
    effect = "Allow"
    actions = [
      "lambda:ListFunctions",
      "lambda:ListTags",
      "ecs:ListClusters",
      "ecs:DescribeClusters",
      "ecs:ListServices",
      "ecs:DescribeServices",
      "eks:ListClusters",
      "eks:DescribeCluster",
      "eks:ListNodegroups",
      "eks:DescribeNodegroup",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "CostAndMetricsDiscovery"
    effect = "Allow"
    actions = [
      "cloudwatch:GetMetricStatistics",
      "cloudwatch:GetMetricData",
      "cloudwatch:ListMetrics",
      "ce:GetCostAndUsage",
      "ce:GetDimensionValues",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role" "cradle_role" {
  name               = "CradleControlPlaneReadOnlyRole"
  assume_role_policy = data.aws_iam_policy_document.cradle_trust.json
  description        = "Read-only cross-account role for Cradle infrastructure discovery"
}

resource "aws_iam_role_policy" "cradle_policy" {
  name   = "CradleReadOnlyDiscoveryPolicy"
  role   = aws_iam_role.cradle_role.id
  policy = data.aws_iam_policy_document.cradle_read_only.json
}

output "cradle_role_arn" {
  description = "Role ARN to enter in the Cradle console"
  value       = aws_iam_role.cradle_role.arn
}
```
