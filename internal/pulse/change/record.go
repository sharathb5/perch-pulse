package change

import (
	"fmt"
	"strings"
	"time"
)

// RecordInput is the smallest useful ingestion path for a change event.
type RecordInput struct {
	Type         ChangeType
	Source       string
	ServiceIDs   []string
	Environment  string
	Repository   string
	CommitSHA    string
	ParentSHA    string
	Branch       string
	PRNumber     *int
	Actor        string
	Title        string
	Summary      string
	CreatedAt    time.Time
	DeployedAt   *time.Time
	ObservedAt   time.Time
	FilesChanged []string
	Metadata     map[string]string
	Mapper       Mapper
	// Simulated marks demo/harness markers that are not real cloud deploys.
	Simulated bool
}

// Record builds, maps, validates, and returns a change Event ready to store.
func Record(in RecordInput) (Event, error) {
	now := time.Now().UTC()
	created := in.CreatedAt
	if created.IsZero() {
		created = now
	} else {
		created = created.UTC()
	}
	observed := in.ObservedAt
	if observed.IsZero() {
		observed = now
	} else {
		observed = observed.UTC()
	}
	var deployed *time.Time
	if in.DeployedAt != nil {
		t := in.DeployedAt.UTC()
		deployed = &t
	} else if in.Type == ChangeDeployment {
		t := observed
		deployed = &t
	}

	src := strings.TrimSpace(in.Source)
	if src == "" {
		if in.Simulated {
			src = "deploy-marker"
		} else {
			src = "cli"
		}
	}

	key := strings.TrimSpace(in.CommitSHA)
	if key == "" {
		key = strings.Join(in.ServiceIDs, ",")
	}
	if key == "" {
		key = string(in.Type)
	}

	e := Event{
		SchemaVersion: SchemaVersion,
		ChangeID:      ChangeIDFor(in.Type, key, observed),
		ChangeType:    in.Type,
		Source:        src,
		Repository:    strings.TrimSpace(in.Repository),
		CommitSHA:     strings.TrimSpace(in.CommitSHA),
		ParentSHA:     strings.TrimSpace(in.ParentSHA),
		Branch:        strings.TrimSpace(in.Branch),
		PRNumber:      in.PRNumber,
		Environment:   strings.TrimSpace(in.Environment),
		ServiceIDs:    append([]string(nil), in.ServiceIDs...),
		Actor:         strings.TrimSpace(in.Actor),
		Title:         strings.TrimSpace(in.Title),
		Summary:       strings.TrimSpace(in.Summary),
		CreatedAt:     created,
		DeployedAt:    deployed,
		ObservedAt:    observed,
		FilesChanged:  append([]string(nil), in.FilesChanged...),
		Metadata:      cloneMeta(in.Metadata),
	}
	if in.Simulated {
		if e.Metadata == nil {
			e.Metadata = map[string]string{}
		}
		e.Metadata["simulated"] = "true"
		e.Evidence = append(e.Evidence, EvidenceRef{
			Ref:     "demo:simulated-deploy-marker",
			Summary: "Synthetic deployment marker for demo/eval; not a real cloud deploy",
		})
		if e.Title == "" {
			e.Title = "Simulated deployment marker"
		}
		if e.Summary == "" {
			e.Summary = "Simulated deployment marker recorded for correlation demo (not a real deploy)"
		}
	}

	in.Mapper.ApplyMapping(&e)
	if len(e.ServiceIDs) == 0 && e.ServiceMapping == "" {
		e.ServiceMapping = MappingUnknown
	}
	if err := e.Validate(); err != nil {
		return Event{}, err
	}
	return e, nil
}

func cloneMeta(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// MustRecord is like Record but panics on error (tests only).
func MustRecord(in RecordInput) Event {
	e, err := Record(in)
	if err != nil {
		panic(fmt.Sprintf("change.MustRecord: %v", err))
	}
	return e
}
