package change

import (
	"fmt"
	"path"
	"strings"
)

// PathRule maps a repository path prefix to a Pulse service ID.
// First matching rule wins. Unmatched paths do not imply all services changed.
type PathRule struct {
	// PathPrefix is matched with path.Clean semantics (e.g. "services/payment/").
	PathPrefix string `json:"path_prefix" yaml:"path_prefix"`
	// ServiceID is the potentially affected Pulse service.
	ServiceID string `json:"service_id" yaml:"service_id"`
}

// Mapper resolves potentially affected services from explicit IDs and path rules.
// It never consults scenario ground truth.
type Mapper struct {
	Rules []PathRule
}

// Resolve returns service IDs and mapping uncertainty for a change.
// Explicit serviceIDs always take precedence when non-empty.
func (m Mapper) Resolve(explicit []string, filesChanged []string) (ids []string, unc MappingUncertainty, note string) {
	var cleaned []string
	seen := map[string]struct{}{}
	for _, id := range explicit {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) > 0 {
		return cleaned, MappingExplicit, "service IDs provided explicitly by caller"
	}

	if len(m.Rules) == 0 || len(filesChanged) == 0 {
		return nil, MappingUnknown, "no explicit service IDs and no usable path mapping"
	}

	matchedFiles := 0
	for _, f := range filesChanged {
		f = path.Clean(strings.TrimSpace(f))
		if f == "." || f == "" {
			continue
		}
		hit := false
		for _, r := range m.Rules {
			prefix := path.Clean(strings.TrimSpace(r.PathPrefix))
			if prefix == "." || prefix == "" || strings.TrimSpace(r.ServiceID) == "" {
				continue
			}
			if f == prefix || strings.HasPrefix(f, prefix+"/") || strings.HasPrefix(f+"/", prefix+"/") {
				id := strings.TrimSpace(r.ServiceID)
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					cleaned = append(cleaned, id)
				}
				hit = true
				break
			}
		}
		if hit {
			matchedFiles++
		}
	}

	switch {
	case len(cleaned) == 0:
		return nil, MappingUnknown, "path rules present but no files matched"
	case matchedFiles < len(filesChanged):
		return cleaned, MappingPartial, fmt.Sprintf("mapped %d/%d files; unmatched paths do not imply other services", matchedFiles, len(filesChanged))
	default:
		return cleaned, MappingConfigured, "service IDs derived from configured path→service rules"
	}
}

// ApplyMapping fills ServiceIDs and ServiceMapping on e when services are unset.
func (m Mapper) ApplyMapping(e *Event) {
	if e == nil {
		return
	}
	if len(e.ServiceIDs) > 0 && e.ServiceMapping == "" {
		e.ServiceMapping = MappingExplicit
		return
	}
	if len(e.ServiceIDs) > 0 {
		return
	}
	ids, unc, note := m.Resolve(nil, e.FilesChanged)
	e.ServiceIDs = ids
	e.ServiceMapping = unc
	if note != "" && e.Summary == "" {
		e.Summary = note
	} else if note != "" && e.Metadata == nil {
		e.Metadata = map[string]string{"mapping_note": note}
	} else if note != "" {
		e.Metadata["mapping_note"] = note
	}
}
