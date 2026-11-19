package inventory

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cradle-dev/cradle/pkg/domain"
)

var fields = []string{"owner", "team", "application", "environment", "purpose", "criticality"}

var tagFields = map[string]string{
	"owner": "owner", "owned-by": "owner", "owner_email": "owner",
	"team": "team", "team-name": "team", "team_name": "team",
	"application": "application", "app": "application", "service": "application",
	"environment": "environment", "env": "environment", "stage": "environment",
	"purpose": "purpose", "business-purpose": "purpose", "description": "purpose",
	"criticality": "criticality", "business-criticality": "criticality",
}

type candidate struct {
	value      string
	source     string
	evidence   string
	precedence int
	priority   int
	confidence float64
}

func unknown(reason string) domain.InventoryAnswer {
	return domain.InventoryAnswer{Value: domain.Unknown, Source: domain.Unknown, Evidence: []string{reason}, Candidates: []string{}}
}

func normalized(field, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, domain.Unknown) {
		return ""
	}
	switch field {
	case "environment":
		switch strings.ToLower(value) {
		case "prod", "production":
			return "production"
		case "stage", "staging", "stg":
			return "staging"
		case "dev", "development":
			return "development"
		default:
			return ""
		}
	case "criticality":
		switch strings.ToLower(value) {
		case "critical", "high", "medium", "low":
			return strings.ToLower(value)
		default:
			return ""
		}
	}
	return value
}

func selectAnswer(candidates []candidate) domain.InventoryAnswer {
	if len(candidates) == 0 {
		return unknown("No explicit tag, matching rule, or recorded ownership supplies this answer.")
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.precedence != b.precedence {
			return a.precedence > b.precedence
		}
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		return a.evidence < b.evidence
	})
	first := candidates[0]
	values := map[string]bool{}
	answer := domain.InventoryAnswer{Value: first.value, Source: first.source, Confidence: first.confidence, Evidence: []string{}, Candidates: []string{}}
	for _, c := range candidates {
		if c.precedence != first.precedence || c.priority != first.priority {
			break
		}
		values[c.value] = true
		answer.Evidence = append(answer.Evidence, c.evidence)
		if c.confidence < answer.Confidence {
			answer.Confidence = c.confidence
		}
	}
	if len(values) > 1 {
		answer.Value, answer.Source, answer.Confidence = domain.Unknown, "CONFLICT", 0
		for value := range values {
			answer.Candidates = append(answer.Candidates, value)
		}
		sort.Strings(answer.Candidates)
	}
	return answer
}

func Resolve(resource domain.ResourceEntity, rules []domain.OwnershipRule, records []domain.OwnershipRecord) *domain.OperationalInventory {
	candidates := map[string][]candidate{}
	tagValues := map[string]map[string]bool{}
	analysis := domain.TagAnalysis{EmptyKeys: []string{}, MissingFields: []string{}, ConflictingFields: []string{}}
	keys := make([]string, 0, len(resource.Tags))
	for key := range resource.Tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := resource.Tags[key]
		if strings.HasPrefix(strings.ToLower(key), "aws:") {
			continue
		}
		if strings.TrimSpace(value) == "" {
			analysis.EmptyKeys = append(analysis.EmptyKeys, key)
			continue
		}
		analysis.UserTagCount++
		field := tagFields[strings.ToLower(strings.TrimSpace(key))]
		if field == "" {
			continue
		}
		value = normalized(field, value)
		if value == "" {
			continue
		}
		if tagValues[field] == nil {
			tagValues[field] = map[string]bool{}
		}
		tagValues[field][value] = true
		candidates[field] = append(candidates[field], candidate{value: value, source: string(domain.ProvenanceObservedTag),
			evidence: fmt.Sprintf("tag:%s = %s", key, resource.Tags[key]), precedence: 1, confidence: 1})
	}
	for _, rule := range rules {
		if rule.TenantID != resource.TenantID || !rule.Enabled {
			continue
		}
		value, exists := resource.Tags[rule.TagKey]
		if !exists || value != rule.TagValue {
			continue
		}
		for field, raw := range rule.Assignments.Fields() {
			value := normalized(field, raw)
			if value == "" {
				continue
			}
			candidates[field] = append(candidates[field], candidate{value: value, source: "OWNERSHIP_RULE", precedence: 2,
				priority: rule.Priority, confidence: 1, evidence: fmt.Sprintf("Rule %s (%s): tag:%s = %s → %s = %s", rule.Name, rule.ID, rule.TagKey, rule.TagValue, field, value)})
		}
	}
	for _, record := range records {
		if record.TenantID != resource.TenantID || record.ResourceID != resource.ID {
			continue
		}
		// Unverified historical inferences must not override current evidence.
		if record.Source != domain.OwnershipSourceManual && !record.IsVerified {
			continue
		}
		field := "owner"
		if record.OwnerType == domain.OwnerTypeTeam {
			field = "team"
		}
		value := normalized(field, record.OwnerID)
		if value == "" {
			continue
		}
		precedence := 3
		if record.IsVerified {
			precedence = 4
		}
		candidates[field] = append(candidates[field], candidate{value: value, source: "OWNERSHIP_RECORD", precedence: precedence,
			confidence: record.Confidence, evidence: fmt.Sprintf("Ownership record %s (%s, verified=%t)", record.ID, record.Source, record.IsVerified)})
	}
	answers := map[string]domain.InventoryAnswer{}
	for _, field := range fields {
		answers[field] = selectAnswer(candidates[field])
		if len(tagValues[field]) == 0 {
			analysis.MissingFields = append(analysis.MissingFields, field)
		}
		if len(tagValues[field]) > 1 || answers[field].Source == "CONFLICT" {
			analysis.ConflictingFields = append(analysis.ConflictingFields, field)
		}
	}
	return &domain.OperationalInventory{Owner: answers["owner"], Team: answers["team"], Application: answers["application"],
		Environment: answers["environment"], Purpose: answers["purpose"], Criticality: answers["criticality"],
		MissingOwner: answers["owner"].Value == domain.Unknown && answers["team"].Value == domain.Unknown,
		Untagged:     analysis.UserTagCount == 0, Orphan: detectOrphan(resource), TagAnalysis: analysis}
}

func detectOrphan(resource domain.ResourceEntity) domain.InventoryAnswer {
	answer := unknown("No resource-specific provider evidence establishes whether this resource is orphaned; missing graph edges alone are insufficient.")
	orphaned, known := false, false
	switch resource.ResourceType {
	case "aws:ec2:volume":
		switch resource.Status {
case domain.ResourceStatusAvailable:
			orphaned, known = true, true
			answer.Evidence = []string{"Provider volume status is available (unattached); review retention intent before acting."}
		case domain.ResourceStatusInUse:
			known = true
			answer.Evidence = []string{"Provider volume status is in_use (attached)."}
		}
	case "aws:ec2:eip":
		instance, hasInstance := resource.ObservedState["instance_id"].(string)
		network, hasNetwork := resource.ObservedState["network_interface_id"].(string)
		if instance != "" || network != "" {
			known = true
			answer.Evidence = []string{"Provider reports an instance or network-interface association."}
		} else if hasInstance && hasNetwork {
			orphaned, known = true, true
			answer.Evidence = []string{"Provider explicitly reports no instance or network-interface association; review reservation intent before acting."}
		}
	}
	if known {
		answer.Value = "NOT_ORPHANED"
		if orphaned {
			answer.Value = "ORPHANED"
		}
		answer.Source, answer.Confidence = string(domain.ProvenanceInferred), 1
	}
	return answer
}
