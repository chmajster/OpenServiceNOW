package serviceportfolio

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/assets"
)

var keyPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{1,63}$`)

var itemTypes = map[string]bool{
	"BUSINESS_APPLICATION": true,
	"SERVICE_INSTANCE":     true,
	"TECHNICAL_SERVICE":    true,
	"BUSINESS_SERVICE":     true,
}

var environments = map[string]bool{
	"DEV": true, "TEST": true, "STAGING": true, "PROD": true, "ALL": true,
}

func normalizeKey(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if !keyPattern.MatchString(value) {
		return "", ErrInvalid
	}
	return value, nil
}

func normalizeItem(in ItemInput) (ItemInput, error) {
	key, err := normalizeKey(in.ServiceKey)
	if err != nil {
		return in, fmt.Errorf("%w: nieprawidłowy stabilny identyfikator usługi", err)
	}
	in.ServiceKey = key
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Scope = strings.TrimSpace(in.Scope)
	in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
	in.Criticality = strings.ToUpper(strings.TrimSpace(in.Criticality))
	in.Environment = strings.ToUpper(strings.TrimSpace(in.Environment))
	in.DependencyMode = strings.ToUpper(strings.TrimSpace(in.DependencyMode))
	if in.DependencyMode == "" {
		in.DependencyMode = "NOT_MODELED"
	}
	if in.Criticality == "" {
		in.Criticality = "MEDIUM"
	}
	if !itemTypes[in.Type] || !assets.Text(in.Name, 1, 160) || !assets.Text(in.Description, 0, 4000) ||
		!assets.Text(in.Scope, 0, 200) || !contains([]string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}, in.Criticality) ||
		!environments[in.Environment] || !contains([]string{"NOT_MODELED", "ALL", "ANY", "QUORUM"}, in.DependencyMode) {
		return in, ErrInvalid
	}
	if in.DependencyMode == "QUORUM" && in.DependencyQuorum == 0 || in.DependencyMode != "QUORUM" && in.DependencyQuorum != 0 {
		return in, ErrInvalid
	}
	if in.CIID == 0 && in.CIVersion != 0 || in.CIID > 0 && in.CIVersion == 0 {
		return in, ErrInvalid
	}
	if in.ClearCILink && in.CIID > 0 {
		return in, ErrInvalid
	}
	return in, nil
}

func normalizeOffer(in OfferInput) (OfferInput, error) {
	key, err := normalizeKey(in.OfferKey)
	if err != nil {
		return in, fmt.Errorf("%w: nieprawidłowy stabilny identyfikator oferty", err)
	}
	in.OfferKey = key
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.AvailabilityConditions = strings.TrimSpace(in.AvailabilityConditions)
	in.ObligationSummary = strings.TrimSpace(in.ObligationSummary)
	in.SupportModel = strings.ToUpper(strings.TrimSpace(in.SupportModel))
	if in.ParentServiceID == 0 || !assets.Text(in.Name, 1, 160) || !assets.Text(in.Description, 0, 4000) ||
		!assets.Text(in.AvailabilityConditions, 0, 1000) || !assets.Text(in.ObligationSummary, 0, 1000) ||
		!contains([]string{"STANDARD", "EXTENDED", "PREMIUM", "CUSTOM"}, in.SupportModel) || len(in.AudienceGroupIDs) > 50 {
		return in, ErrInvalid
	}
	seen := map[string]bool{}
	for i := range in.AudienceGroupIDs {
		id := strings.TrimSpace(in.AudienceGroupIDs[i])
		parsed, parseErr := strconv.ParseUint(id, 10, 64)
		if parseErr != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != id || seen[id] {
			return in, ErrInvalid
		}
		seen[id] = true
		in.AudienceGroupIDs[i] = id
	}
	return in, nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func relationAllowed(sourceType, relationType, targetType, targetCIClass string) bool {
	switch relationType {
	case "REALIZES":
		return sourceType == "SERVICE_INSTANCE" && targetType == "BUSINESS_APPLICATION" && targetCIClass == ""
	case "DEPENDS_ON":
		if targetCIClass != "" {
			return sourceType == "SERVICE_INSTANCE" || sourceType == "TECHNICAL_SERVICE" || sourceType == "BUSINESS_SERVICE"
		}
		switch sourceType {
		case "SERVICE_INSTANCE":
			return targetType == "TECHNICAL_SERVICE"
		case "BUSINESS_SERVICE":
			return targetType == "SERVICE_INSTANCE" || targetType == "TECHNICAL_SERVICE"
		default:
			return false
		}
	case "MAPS_TO_CI":
		return targetCIClass != "" && targetType == ""
	default:
		return false
	}
}

func lookupNextID(item map[string]any) (uint64, bool) {
	value, ok := item["id"]
	if !ok {
		return 0, false
	}
	switch id := value.(type) {
	case string:
		parsed, err := strconv.ParseUint(id, 10, 64)
		if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != id {
			return 0, false
		}
		return parsed, true
	case uint64:
		return id, id > 0
	}
	return 0, false
}

func dependencyAvailability(mode string, quorum uint16, states []string) (string, string) {
	if mode == "NOT_MODELED" || len(states) == 0 {
		return "NOT_MODELED", "no_redundancy_model"
	}
	available, degraded, unavailable, unknown := 0, 0, 0, 0
	for _, state := range states {
		switch state {
		case "AVAILABLE":
			available++
		case "DEGRADED":
			degraded++
		case "UNAVAILABLE":
			unavailable++
		default:
			unknown++
		}
	}
	switch mode {
	case "ALL":
		if unavailable > 0 {
			return "UNAVAILABLE", "all_dependencies_required"
		}
		if unknown > 0 {
			return "UNKNOWN", "all_dependencies_required"
		}
		if degraded > 0 {
			return "DEGRADED", "all_dependencies_required"
		}
		return "AVAILABLE", "all_dependencies_required"
	case "ANY":
		if available > 0 {
			if degraded+unavailable > 0 {
				return "DEGRADED", "any_dependency_sufficient"
			}
			return "AVAILABLE", "any_dependency_sufficient"
		}
		if degraded > 0 {
			return "DEGRADED", "any_dependency_sufficient"
		}
		if unavailable == len(states) {
			return "UNAVAILABLE", "any_dependency_sufficient"
		}
		return "UNKNOWN", "any_dependency_sufficient"
	case "QUORUM":
		counted := available + degraded
		if counted >= int(quorum) {
			if degraded > 0 || unavailable > 0 {
				return "DEGRADED", "quorum_dependency_count"
			}
			return "AVAILABLE", "quorum_dependency_count"
		}
		if counted+unknown < int(quorum) {
			return "UNAVAILABLE", "quorum_dependency_count"
		}
		return "UNKNOWN", "quorum_dependency_count"
	default:
		return "NOT_MODELED", "no_redundancy_model"
	}
}
