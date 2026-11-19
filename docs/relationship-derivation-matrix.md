# Cradle Relationship Derivation Matrix & Graph Topology Specification

This document defines the mathematical and epistemological rules for synthesizing, resolving, and traversing infrastructure relationships in Cradle.

---

## 1. Epistemological Foundations & Integrity Constraints

Cradle adheres to a strict principle of **zero fabricated relationships**. Relationships are never invented to make graphs appear connected. Every edge in the infrastructure graph must be backed by an auditable provenance chain.

### Provenance Hierarchy

| Provenance Level | Description | Confidence Default | Mutates Config Hash? |
| :--- | :--- | :--- | :--- |
| `OBSERVED_FROM_PROVIDER` | Fact directly emitted by cloud provider API (e.g., EC2 instance `SubnetId` or route table association). | `1.00` (100%) | Yes |
| `OBSERVED_FROM_TAG` | Association established through structured provider tags (e.g., EKS managed nodegroup tag `eks:nodegroup-name` on EC2 worker nodes). | `0.95` (95%) | Yes (via tags map) |
| `DERIVED_BY_CRADLE` | Edge derived deterministically from multi-resource topological rules (e.g., CIDR non-overlapping routing or peering). | `0.90` (90%) | No |
| `INFERRED_BY_CRADLE` | Heuristic association inferred from naming conventions or traffic telemetry. | `< 0.90` | No |

---

## 2. Infrastructure Relationship Derivation Matrix

| Source Resource Type | Edge Type | Destination Resource Type | AWS SDK API / Field Source | Confidence | Provenance | Derivation Logic & Invariant |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `aws:ec2:instance` | `ATTACHED_TO` | `aws:ec2:subnet` | `ec2.DescribeInstances` &rarr; `Instance.SubnetId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Instance is bound to an availability zone and subnet interface. |
| `aws:ec2:instance` | `ATTACHED_TO` | `aws:ec2:vpc` | `ec2.DescribeInstances` &rarr; `Instance.VpcId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Instance resides inside the specified VPC boundary. |
| `aws:ec2:instance` | `ATTACHED_TO` | `aws:ec2:security-group` | `ec2.DescribeInstances` &rarr; `Instance.SecurityGroups[].GroupId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Direct firewall attachment to primary and secondary ENIs. |
| `aws:ec2:volume` | `ATTACHED_TO` | `aws:ec2:instance` | `ec2.DescribeVolumes` &rarr; `Volume.Attachments[].InstanceId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Block device attachment. If unattached, flagged by Waste Detector. |
| `aws:ec2:snapshot` | `DEPENDS_ON` | `aws:ec2:volume` | `ec2.DescribeSnapshots` &rarr; `Snapshot.VolumeId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Lineage of snapshot point-in-time recovery. |
| `aws:ec2:eip` | `ATTACHED_TO` | `aws:ec2:instance` | `ec2.DescribeAddresses` &rarr; `Address.InstanceId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Elastic IP bound to EC2 instance public ENI. |
| `aws:ec2:route-table` | `ATTACHED_TO` | `aws:ec2:vpc` | `ec2.DescribeRouteTables` &rarr; `RouteTable.VpcId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Route table belongs exclusively to parent VPC. |
| `aws:ec2:subnet` | `ROUTES_TO` | `aws:ec2:route-table` | `ec2.DescribeRouteTables` &rarr; `Association.SubnetId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Subnet traffic routing table association. |
| `aws:ec2:route-table` | `ROUTES_TO` | `aws:ec2:nat-gateway` | `ec2.DescribeRouteTables` &rarr; `Route.NatGatewayId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Outbound egress routing target via NAT Gateway. |
| `aws:ec2:nat-gateway` | `ATTACHED_TO` | `aws:ec2:subnet` | `ec2.DescribeNatGateways` &rarr; `NatGateway.SubnetId` | `1.00` | `OBSERVED_FROM_PROVIDER` | NAT Gateway network interface provisioned in public subnet. |
| `aws:ec2:nat-gateway` | `ATTACHED_TO` | `aws:ec2:vpc` | `ec2.DescribeNatGateways` &rarr; `NatGateway.VpcId` | `1.00` | `OBSERVED_FROM_PROVIDER` | NAT Gateway VPC boundary. |
| `aws:ec2:nat-gateway` | `ATTACHED_TO` | `aws:ec2:eip` | `ec2.DescribeNatGateways` &rarr; `NatGatewayAddresses[].AllocationId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Public Elastic IP bound to NAT Gateway egress. |
| `aws:ec2:security-group` | `ATTACHED_TO` | `aws:ec2:vpc` | `ec2.DescribeSecurityGroups` &rarr; `SecurityGroup.VpcId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Security group belongs exclusively to parent VPC. |
| `aws:rds:db-instance` | `ATTACHED_TO` | `aws:ec2:vpc` | `rds.DescribeDBInstances` &rarr; `DBSubnetGroup.VpcId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Database provisioned inside tenant VPC network. |
| `aws:rds:db-instance` | `ATTACHED_TO` | `aws:ec2:subnet` | `rds.DescribeDBInstances` &rarr; `DBSubnetGroup.Subnets[].SubnetIdentifier` | `1.00` | `OBSERVED_FROM_PROVIDER` | DB Subnet Group multi-AZ subnet attachments. |
| `aws:rds:db-instance` | `ATTACHED_TO` | `aws:ec2:security-group` | `rds.DescribeDBInstances` &rarr; `VpcSecurityGroups[].VpcSecurityGroupId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Database firewall access control rules. |
| `aws:rds:db-instance` | `ENCRYPTS_WITH` | `aws:kms:key` | `rds.DescribeDBInstances` &rarr; `KmsKeyId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Storage at-rest envelope encryption master key. |
| `aws:rds:db-cluster` | `ATTACHED_TO` | `aws:ec2:security-group` | `rds.DescribeDBClusters` &rarr; `VpcSecurityGroups[].VpcSecurityGroupId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Aurora cluster network firewall association. |
| `aws:rds:db-cluster` | `ENCRYPTS_WITH` | `aws:kms:key` | `rds.DescribeDBClusters` &rarr; `KmsKeyId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Aurora cluster KMS master key. |
| `aws:elbv2:load-balancer` | `ATTACHED_TO` | `aws:ec2:vpc` | `elbv2.DescribeLoadBalancers` &rarr; `LoadBalancer.VpcId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Load Balancer network perimeter. |
| `aws:elbv2:load-balancer` | `ATTACHED_TO` | `aws:ec2:subnet` | `elbv2.DescribeLoadBalancers` &rarr; `AvailabilityZones[].SubnetId` | `1.00` | `OBSERVED_FROM_PROVIDER` | High-availability load balancer interfaces across subnets. |
| `aws:elbv2:load-balancer` | `ATTACHED_TO` | `aws:ec2:security-group` | `elbv2.DescribeLoadBalancers` &rarr; `SecurityGroups[]` | `1.00` | `OBSERVED_FROM_PROVIDER` | ALB public/internal ingress firewall. |
| `aws:elbv2:load-balancer` | `ROUTES_TO` | `aws:elbv2:target-group` | `elbv2.DescribeTargetGroups` &rarr; `TargetGroup.LoadBalancerArns[]` | `1.00` | `OBSERVED_FROM_PROVIDER` | Traffic forwarding rule from LB listener to Target Group. |
| `aws:elbv2:target-group` | `ATTACHED_TO` | `aws:ec2:vpc` | `elbv2.DescribeTargetGroups` &rarr; `TargetGroup.VpcId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Target group network scope. |
| `aws:elbv2:target-group` | `TARGETS` | `aws:ec2:instance` | `elbv2.DescribeTargetHealth` &rarr; `TargetHealthDescriptions[].Target.Id` | `1.00` | `OBSERVED_FROM_PROVIDER` | Live health-monitored forwarding target. |
| `aws:eks:cluster` | `ATTACHED_TO` | `aws:ec2:vpc` | `eks.DescribeCluster` &rarr; `ResourcesVpcConfig.VpcId` | `1.00` | `OBSERVED_FROM_PROVIDER` | Kubernetes control plane ENI in VPC. |
| `aws:eks:cluster` | `ATTACHED_TO` | `aws:ec2:subnet` | `eks.DescribeCluster` &rarr; `ResourcesVpcConfig.SubnetIds[]` | `1.00` | `OBSERVED_FROM_PROVIDER` | Kubernetes control plane subnet attachments. |
| `aws:eks:cluster` | `CONTAINS` | `aws:eks:nodegroup` | `eks.ListNodegroups` &rarr; `ClusterName` | `1.00` | `OBSERVED_FROM_PROVIDER` | Managed worker node pool containment. |
| `aws:eks:nodegroup` | `ATTACHED_TO` | `aws:ec2:subnet` | `eks.DescribeNodegroup` &rarr; `Nodegroup.Subnets[]` | `1.00` | `OBSERVED_FROM_PROVIDER` | Subnets hosting worker nodes. |
| `aws:eks:nodegroup` | `MANAGES` | `aws:ec2:instance` | Provider Tag Match: `eks:nodegroup-name = <nodegroup_name>` | `0.95` | `OBSERVED_FROM_TAG` | AWS EKS tags EC2 instances upon worker join. |

---

## 3. Two-Phase Graph Resolution Architecture

Cross-resource relationships frequently span distinct AWS discovery APIs (e.g., an EC2 instance ID discovered in an ELB target health check before the EC2 inventory runs).

To guarantee consistency, Cradle utilizes a **Two-Phase Graph Resolver**:

```
[Phase 1: Discovery Adapters]
  - Emit ResourceEntities with canonical UUIDs
  - Synthesize candidate edges with unresolved TargetARN / TargetNativeID / Tag Match props
          |
          v
[Phase 2: Graph Resolver (pkg/graph/resolver.go)]
  1. Build in-memory tenant index of (ARN -> UUID) and (NativeID -> UUID)
  2. Resolve Tag-Based candidate relationships (e.g. tags->>'eks:nodegroup-name' = $1)
  3. Resolve TargetResourceID via Native ID / ARN lookup
  4. Write validated edges to database table resource_relationships
```

---

## 4. Graph Navigation: Directional Traversal Semantics

Cradle models infrastructure relationships as a **directed acyclic or cyclic multigraph** $G = (V, E)$, where each directed edge $e = (u, v) \in E$ has type $T(e)$ and source $u \to$ destination $v$.

When analyzing dependencies, Cradle distinguishes between two key operational views:

### 4.1 Upstream Dependencies (Incoming Edges: $\bullet \to u$)
- **Question Answered**: *"Who depends on me? What will break if I terminate this resource?"*
- **Mathematical Definition**: $\{v \in V \mid (v, u) \in E\}$
- **Example**: For an EC2 subnet, its upstream dependents include the EC2 instances, RDS databases, and NAT Gateways attached to it.

### 4.2 Downstream Dependencies (Outgoing Edges: $u \to \bullet$)
- **Question Answered**: *"What do I rely on? What infrastructure prerequisites must exist for me to function?"*
- **Mathematical Definition**: $\{w \in V \mid (u, w) \in E\}$
- **Example**: For an EC2 instance, its downstream dependencies include its attached Subnet, parent VPC, Security Groups, and EBS Volumes.

---

## 5. Multi-Provider Extension Pattern

To add relationship support for additional cloud providers (e.g. GCP, Azure, Kubernetes):

1. **Implement Canonical Resource Types**: Follow `provider:service:resource` syntax (e.g. `gcp:compute:instance`, `azure:network:vnet`).
2. **Reuse Standard Relationship Types**:
   - `CONTAINS` for network/resource group scopes
   - `ATTACHED_TO` for NICs, subnets, and firewalls
   - `ROUTES_TO` for routing tables and gateways
   - `TARGETS` for load balancer backend pools
   - `ENCRYPTS_WITH` for Cloud KMS / Key Vault keys
3. **Emit Candidate Relationships**: Set `TargetNativeID` or `TargetARN`, assign `Confidence` (`1.0` for observed facts, `0.95` for tag matches), and set `Provenance`.
4. **Resolver Persistence**: The existing `pkg/graph/resolver.go` and `pkg/graph/queries.go` are completely cloud-agnostic and will resolve and navigate the new provider's edges without changes.
