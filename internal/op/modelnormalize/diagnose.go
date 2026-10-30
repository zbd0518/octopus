package modelnormalize

import (
	"sort"
	"strings"
)

type CoverageCluster struct {
	Canonical string   `json:"canonical"`
	Variants  []string `json:"variants"`
}

type CoverageReport struct {
	InputCount         int               `json:"input_count"`
	CanonicalCount     int               `json:"canonical_count"`
	MergedClusterCount int               `json:"merged_cluster_count"`
	MergedVariantCount int               `json:"merged_variant_count"`
	UnchangedCount     int               `json:"unchanged_count"`
	Unresolved         []string          `json:"unresolved,omitempty"`
	Clusters           []CoverageCluster `json:"clusters,omitempty"`
	HighRiskMerges     []CoverageCluster `json:"high_risk_merges,omitempty"`
	Diagnostics        []RuleDiagnostic  `json:"diagnostics,omitempty"`
}

// AnalyzeNames applies one rule set to a model-name corpus and returns a
// deterministic coverage report. The input is deduplicated case-sensitively
// while variants differing only by case remain visible in their cluster.
func AnalyzeNames(names []string, rules Rules) CoverageReport {
	if rules.explicitFullByKey == nil || rules.explicitBaseByKey == nil {
		rules.explicitFullByKey, rules.explicitBaseByKey = buildExplicitByKey(rules.ExplicitMappings, rules)
	}

	clusters := make(map[string]map[string]struct{})
	seenNames := make(map[string]struct{}, len(names))
	canonicalByName := make(map[string]string, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := seenNames[name]; exists {
			continue
		}
		seenNames[name] = struct{}{}
		canonical := NormalizeWithRules(name, rules)
		if canonical == "" {
			continue
		}
		canonicalByName[name] = canonical
		if clusters[canonical] == nil {
			clusters[canonical] = make(map[string]struct{})
		}
		clusters[canonical][name] = struct{}{}
	}

	report := CoverageReport{
		InputCount:     len(seenNames),
		CanonicalCount: len(clusters),
		Diagnostics:    DiagnoseRules(rules),
	}
	for canonical, variants := range clusters {
		if len(variants) == 1 {
			report.UnchangedCount++
		}
		if len(variants) <= 1 {
			continue
		}
		report.MergedClusterCount++
		report.MergedVariantCount += len(variants) - 1
		ordered := make([]string, 0, len(variants))
		for variant := range variants {
			ordered = append(ordered, variant)
		}
		sort.Strings(ordered)
		cluster := CoverageCluster{
			Canonical: canonical,
			Variants:  ordered,
		}
		report.Clusters = append(report.Clusters, cluster)
		if clusterHasSemanticDifference(ordered) {
			report.HighRiskMerges = append(report.HighRiskMerges, cluster)
		}
	}

	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if canonical, ok := canonicalByName[name]; !ok || canonical == "" {
			report.Unresolved = append(report.Unresolved, name)
		}
	}
	sort.Strings(report.Unresolved)
	sort.Slice(report.Clusters, func(i, j int) bool {
		if len(report.Clusters[i].Variants) != len(report.Clusters[j].Variants) {
			return len(report.Clusters[i].Variants) > len(report.Clusters[j].Variants)
		}
		return report.Clusters[i].Canonical < report.Clusters[j].Canonical
	})
	sort.Slice(report.HighRiskMerges, func(i, j int) bool {
		if len(report.HighRiskMerges[i].Variants) != len(report.HighRiskMerges[j].Variants) {
			return len(report.HighRiskMerges[i].Variants) > len(report.HighRiskMerges[j].Variants)
		}
		return report.HighRiskMerges[i].Canonical < report.HighRiskMerges[j].Canonical
	})
	return report
}

func clusterHasSemanticDifference(variants []string) bool {
	if len(variants) < 2 {
		return false
	}
	var (
		first     string
		seenFirst bool
	)
	for _, variant := range variants {
		signature := protectedTokenSignature(variant)
		if !seenFirst {
			first = signature
			seenFirst = true
			continue
		}
		if signature != first {
			return true
		}
	}
	return false
}

func protectedTokenSignature(name string) string {
	normalized := strings.NewReplacer(
		".", "-", "_", "-", ":", "-", "@", "-",
		"/", "-", "(", "-", ")", "-", "[", "-", "]", "-",
	).Replace(strings.ToLower(name))
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}
	padded := "-" + strings.Trim(normalized, "-") + "-"

	protected := make([]string, 0, len(protectedFunctionalTokens))
	for token := range protectedFunctionalTokens {
		needle := "-" + token + "-"
		if !strings.Contains(padded, needle) {
			continue
		}
		// "non-reasoning" is a distinct mode. Do not also record the
		// nested "reasoning" token, otherwise both modes get one signature.
		if token == "reasoning" && strings.Contains(padded, "-non-reasoning-") {
			continue
		}
		protected = append(protected, token)
	}
	sort.Strings(protected)
	return strings.Join(protected, "\x00")
}
