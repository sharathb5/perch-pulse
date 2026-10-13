package export

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/eval"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

func envelope(dataset, dedupeKey, sourceSchema string, prov ProvenanceClass, exportedAt time.Time, observationAt *time.Time) Envelope {
	return Envelope{
		ExportSchemaVersion: ExportSchemaVersion,
		Dataset:             dataset,
		DedupeKey:           dedupeKey,
		ContentFingerprint:  "",
		SourceSchemaVersion: sourceSchema,
		ProvenanceClass:     prov,
		ExportedAt:          exportedAt.UTC(),
		ObservationAt:       observationAt,
	}
}

func incidentRows(incidents []incident.Incident, prov ProvenanceClass, exportedAt time.Time) ([]IncidentRow, []IncidentSignalRow, []string, error) {
	sort.SliceStable(incidents, func(i, j int) bool {
		return incidents[i].IncidentID < incidents[j].IncidentID
	})
	var rows []IncidentRow
	var signals []IncidentSignalRow
	var corrupt []string
	for _, inc := range incidents {
		if err := inc.Validate(); err != nil {
			corrupt = append(corrupt, fmt.Sprintf("incident %s: %v", inc.IncidentID, err))
			continue
		}
		obs := inc.FirstDetectedAt.UTC()
		row := IncidentRow{
			Envelope:           envelope(DatasetIncidents, inc.IncidentID, inc.SchemaVersion, prov, exportedAt, &obs),
			IncidentID:         inc.IncidentID,
			PrimaryServiceID:   inc.PrimaryServiceID,
			Environment:        inc.Environment,
			FindingIDs:         append([]string(nil), inc.FindingIDs...),
			SignalTypes:        append([]string(nil), inc.SignalTypes...),
			FirstDetectedAt:    inc.FirstDetectedAt.UTC(),
			LastObservedAt:     inc.LastObservedAt.UTC(),
			RecoveredAt:        cloneTime(inc.RecoveredAt),
			AffectedServiceIDs: append([]string(nil), inc.AffectedServiceIDs...),
			ObservationCount:   len(inc.Observations),
			InferenceCount:     len(inc.Inferences),
			LimitationCount:    len(inc.Limitations),
			EvidenceRefCount:   len(inc.Evidence),
			SummaryPresent:     strings.TrimSpace(inc.Summary) != "",
			DuringDigestCount:  len(inc.During.Digests),
		}
		if err := attachFingerprint(&row.Envelope, row); err != nil {
			corrupt = append(corrupt, fmt.Sprintf("incident %s fingerprint: %v", inc.IncidentID, err))
			continue
		}
		rows = append(rows, row)
		for _, phase := range []incident.PhaseSnapshot{inc.Before, inc.During, inc.After} {
			for _, d := range phase.Digests {
				key := inc.IncidentID + "|" + phase.Phase + "|" + string(d.Signal)
				fa := d.FirstAt.UTC()
				srow := IncidentSignalRow{
					Envelope:         envelope(DatasetIncidentSignals, key, inc.SchemaVersion, prov, exportedAt, &fa),
					IncidentID:       inc.IncidentID,
					PrimaryServiceID: inc.PrimaryServiceID,
					Environment:      inc.Environment,
					Phase:            phase.Phase,
					Signal:           string(d.Signal),
					SampleCount:      d.Count,
					Median:           d.Median,
					Min:              d.Min,
					Max:              d.Max,
					FirstAt:          fa,
					LastAt:           d.LastAt.UTC(),
				}
				if err := attachFingerprint(&srow.Envelope, srow); err != nil {
					corrupt = append(corrupt, fmt.Sprintf("signal %s: %v", key, err))
					continue
				}
				signals = append(signals, srow)
			}
		}
	}
	sort.SliceStable(signals, func(i, j int) bool { return signals[i].DedupeKey < signals[j].DedupeKey })
	return rows, signals, corrupt, nil
}

func investigationRows(invs []investigation.Investigation, prov ProvenanceClass, exportedAt time.Time) (
	[]InvestigationRow, []HypothesisRow, []CitationRow, []string, error,
) {
	sort.SliceStable(invs, func(i, j int) bool { return invs[i].InvestigationID < invs[j].InvestigationID })
	var rows []InvestigationRow
	var hyps []HypothesisRow
	var cites []CitationRow
	var corrupt []string
	for _, inv := range invs {
		if err := inv.Validate(); err != nil {
			corrupt = append(corrupt, fmt.Sprintf("investigation %s: %v", inv.InvestigationID, err))
			continue
		}
		obs := inv.EvidenceCollectedAt.UTC()
		row := InvestigationRow{
			Envelope:              envelope(DatasetInvestigations, inv.InvestigationID, inv.SchemaVersion, prov, exportedAt, &obs),
			InvestigationID:       inv.InvestigationID,
			IncidentID:            inv.IncidentID,
			Environment:           inv.Environment,
			AffectedServiceIDs:    append([]string(nil), inv.AffectedServiceIDs...),
			IncidentState:         inv.IncidentState,
			GeneratedAt:           inv.GeneratedAt.UTC(),
			EvidenceCollectedAt:   inv.EvidenceCollectedAt.UTC(),
			EvidenceFreshness:     inv.EvidenceFreshness,
			GenerationMethod:      inv.GenerationMethod,
			SourceFingerprint:     inv.ContentFingerprint,
			SymptomCount:          len(inv.ObservedSymptoms),
			HypothesisCount:       len(inv.Hypotheses),
			CitationRefCount:      len(inv.SourceReferences),
			ChangeCandidateCount:  len(inv.ChangeCandidates),
			StrongChangeCandidate: inv.StrongChangeCandidate,
			LinkedRunCount:        len(inv.LinkedRunPulse),
			Diagnosed:             inv.Diagnosed,
			MissingEvidenceCount:  len(inv.MissingEvidence),
			LimitationCount:       len(inv.Limitations),
		}
		if err := attachFingerprint(&row.Envelope, row); err != nil {
			corrupt = append(corrupt, fmt.Sprintf("investigation %s fingerprint: %v", inv.InvestigationID, err))
			continue
		}
		rows = append(rows, row)
		for _, h := range inv.Hypotheses {
			hr, err := hypothesisFromInvestigation(h, inv, false, prov, exportedAt)
			if err != nil {
				corrupt = append(corrupt, err.Error())
				continue
			}
			hyps = append(hyps, hr)
		}
		seenCite := map[string]bool{}
		for _, ref := range inv.SourceReferences {
			key := inv.InvestigationID + "|" + ref.Ref
			if seenCite[key] {
				continue
			}
			seenCite[key] = true
			crow := CitationRow{
				Envelope:        envelope(DatasetCitations, key, inv.SchemaVersion, prov, exportedAt, &obs),
				InvestigationID: inv.InvestigationID,
				IncidentID:      inv.IncidentID,
				Ref:             ref.Ref,
				Classification:  string(ref.Classification),
				SummaryPresent:  strings.TrimSpace(ref.Summary) != "",
			}
			if err := attachFingerprint(&crow.Envelope, crow); err != nil {
				corrupt = append(corrupt, fmt.Sprintf("citation %s: %v", key, err))
				continue
			}
			cites = append(cites, crow)
		}
	}
	sort.SliceStable(hyps, func(i, j int) bool { return hyps[i].DedupeKey < hyps[j].DedupeKey })
	sort.SliceStable(cites, func(i, j int) bool { return cites[i].DedupeKey < cites[j].DedupeKey })
	return rows, hyps, cites, corrupt, nil
}

func hypothesisFromInvestigation(h investigation.Hypothesis, inv investigation.Investigation, rejected bool, prov ProvenanceClass, exportedAt time.Time) (HypothesisRow, error) {
	key := fmt.Sprintf("investigation|%s|%s|%t", inv.InvestigationID, h.HypothesisID, rejected)
	obs := inv.GeneratedAt.UTC()
	row := HypothesisRow{
		Envelope:                     envelope(DatasetHypotheses, key, inv.SchemaVersion, pickHypProv(prov), exportedAt, &obs),
		HypothesisID:                 h.HypothesisID,
		ParentKind:                   "investigation",
		ParentID:                     inv.InvestigationID,
		IncidentID:                   inv.IncidentID,
		InvestigationID:              inv.InvestigationID,
		GenerationMethod:             inv.GenerationMethod,
		Classification:               string(h.Classification),
		Kind:                         h.Kind,
		StatementHash:                statementHash(h.Statement),
		StatementRuneCount:           runeCount(h.Statement),
		SupportingEvidenceRefs:       append([]string(nil), h.SupportingEvidenceRefs...),
		CounterEvidenceRefs:          append([]string(nil), h.CounterEvidenceRefs...),
		MissingEvidenceCount:         len(h.MissingEvidence),
		SuggestedVerificationPresent: strings.TrimSpace(h.SuggestedVerification) != "",
		Rejected:                     rejected,
	}
	if err := attachFingerprint(&row.Envelope, row); err != nil {
		return HypothesisRow{}, fmt.Errorf("hypothesis %s: %w", key, err)
	}
	return row, nil
}

func pickHypProv(base ProvenanceClass) ProvenanceClass {
	if base == ProvenanceUnknown {
		return ProvenanceUnknown
	}
	return base
}

func proposalRows(props []llm.Proposal, exportedAt time.Time) ([]ProposalRow, []HypothesisRow, []string, error) {
	sort.SliceStable(props, func(i, j int) bool { return props[i].ProposalID < props[j].ProposalID })
	var rows []ProposalRow
	var hyps []HypothesisRow
	var corrupt []string
	for _, p := range props {
		if err := p.Validate(); err != nil {
			corrupt = append(corrupt, fmt.Sprintf("proposal %s: %v", p.ProposalID, err))
			continue
		}
		obs := p.GeneratedAt.UTC()
		row := ProposalRow{
			Envelope:                envelope(DatasetProposals, p.ProposalID, p.SchemaVersion, ProvenanceModelGenerated, exportedAt, &obs),
			ProposalID:              p.ProposalID,
			SourceInvestigationID:   p.SourceInvestigationID,
			SourceIncidentID:        p.SourceIncidentID,
			SourceFingerprint:       p.SourceFingerprint,
			GenerationMethod:        p.GenerationMethod,
			GeneratedAt:             p.GeneratedAt.UTC(),
			Diagnosed:               p.Diagnosed,
			HypothesisCount:         len(p.Hypotheses),
			RejectedHypothesisCount: len(p.RejectedHypotheses),
			MissingEvidenceCount:    len(p.MissingEvidence),
			UnknownCount:            len(p.Unknowns),
			LimitationCount:         len(p.Limitations),
			Provider:                p.Provenance.Provider,
			Model:                   p.Provenance.Model,
			EndpointHost:            p.Provenance.EndpointHost,
			FinishReason:            p.Provenance.FinishReason,
			PromptUsageUnits:        p.Provenance.PromptUsageUnits,
			CompleteUsageUnits:      p.Provenance.CompleteUsageUnits,
			RequestID:               p.Provenance.RequestID,
		}
		if err := attachFingerprint(&row.Envelope, row); err != nil {
			corrupt = append(corrupt, fmt.Sprintf("proposal %s fingerprint: %v", p.ProposalID, err))
			continue
		}
		rows = append(rows, row)
		for _, h := range p.Hypotheses {
			hr, err := hypothesisFromProposal(h, p, false, exportedAt)
			if err != nil {
				corrupt = append(corrupt, err.Error())
				continue
			}
			hyps = append(hyps, hr)
		}
		for _, h := range p.RejectedHypotheses {
			hr, err := hypothesisFromProposal(h, p, true, exportedAt)
			if err != nil {
				corrupt = append(corrupt, err.Error())
				continue
			}
			hyps = append(hyps, hr)
		}
	}
	sort.SliceStable(hyps, func(i, j int) bool { return hyps[i].DedupeKey < hyps[j].DedupeKey })
	return rows, hyps, corrupt, nil
}

func hypothesisFromProposal(h llm.Hypothesis, p llm.Proposal, rejected bool, exportedAt time.Time) (HypothesisRow, error) {
	key := fmt.Sprintf("proposal|%s|%s|%t", p.ProposalID, h.HypothesisID, rejected)
	obs := p.GeneratedAt.UTC()
	row := HypothesisRow{
		Envelope:                     envelope(DatasetHypotheses, key, p.SchemaVersion, ProvenanceModelGenerated, exportedAt, &obs),
		HypothesisID:                 h.HypothesisID,
		ParentKind:                   "proposal",
		ParentID:                     p.ProposalID,
		IncidentID:                   p.SourceIncidentID,
		InvestigationID:              p.SourceInvestigationID,
		ProposalID:                   p.ProposalID,
		GenerationMethod:             p.GenerationMethod,
		Classification:               string(h.Classification),
		Kind:                         h.Kind,
		StatementHash:                statementHash(h.Statement),
		StatementRuneCount:           runeCount(h.Statement),
		SupportingEvidenceRefs:       append([]string(nil), h.SupportingEvidenceRefs...),
		CounterEvidenceRefs:          append([]string(nil), h.CounterEvidenceRefs...),
		MissingEvidenceCount:         len(h.MissingEvidence),
		SuggestedVerificationPresent: strings.TrimSpace(h.SuggestedVerification) != "",
		SupportStatus:                h.SupportStatus,
		Rejected:                     rejected,
	}
	if err := attachFingerprint(&row.Envelope, row); err != nil {
		return HypothesisRow{}, fmt.Errorf("hypothesis %s: %w", key, err)
	}
	return row, nil
}

func feedbackFromInvestigation(fbs []investigation.Feedback, exportedAt time.Time) ([]FeedbackRow, []string) {
	sort.SliceStable(fbs, func(i, j int) bool { return fbs[i].FeedbackID < fbs[j].FeedbackID })
	var rows []FeedbackRow
	var corrupt []string
	for _, fb := range fbs {
		if fb.SchemaVersion != investigation.FeedbackSchemaVersion {
			corrupt = append(corrupt, fmt.Sprintf("feedback %s: bad schema", fb.FeedbackID))
			continue
		}
		if strings.TrimSpace(fb.FeedbackID) == "" || strings.TrimSpace(fb.InvestigationID) == "" {
			corrupt = append(corrupt, "feedback missing ids")
			continue
		}
		key := "investigation|" + fb.FeedbackID
		ts := fb.Timestamp.UTC()
		row := FeedbackRow{
			Envelope:              envelope(DatasetInvestigationFeedback, key, fb.SchemaVersion, ProvenanceHumanAssessment, exportedAt, &ts),
			FeedbackID:            fb.FeedbackID,
			TargetKind:            "investigation",
			TargetID:              fb.InvestigationID,
			InvestigationID:       fb.InvestigationID,
			IncidentID:            fb.IncidentID,
			Timestamp:             ts,
			Assessment:            fb.Assessment,
			UsefulHypothesisIDs:   append([]string(nil), fb.UsefulHypothesisIDs...),
			RejectedHypothesisIDs: append([]string(nil), fb.RejectedHypothesisIDs...),
			RationalePresent:      strings.TrimSpace(fb.Rationale) != "",
			RationaleRuneCount:    runeCount(fb.Rationale),
			ReviewerCausePresent:  strings.TrimSpace(fb.ReviewerConfirmedCause) != "",
			ConfirmationMethod:    fb.ConfirmationMethod,
			LimitationCount:       len(fb.Limitations),
		}
		if err := attachFingerprint(&row.Envelope, row); err != nil {
			corrupt = append(corrupt, fmt.Sprintf("feedback %s: %v", fb.FeedbackID, err))
			continue
		}
		rows = append(rows, row)
	}
	return rows, corrupt
}

func feedbackFromProposal(fbs []llm.Feedback, exportedAt time.Time) ([]FeedbackRow, []string) {
	sort.SliceStable(fbs, func(i, j int) bool { return fbs[i].FeedbackID < fbs[j].FeedbackID })
	var rows []FeedbackRow
	var corrupt []string
	for _, fb := range fbs {
		if fb.SchemaVersion != llm.ProposalFeedbackSchemaVersion {
			corrupt = append(corrupt, fmt.Sprintf("proposal feedback %s: bad schema", fb.FeedbackID))
			continue
		}
		key := "proposal|" + fb.FeedbackID
		ts := fb.Timestamp.UTC()
		row := FeedbackRow{
			Envelope:              envelope(DatasetInvestigationFeedback, key, fb.SchemaVersion, ProvenanceHumanAssessment, exportedAt, &ts),
			FeedbackID:            fb.FeedbackID,
			TargetKind:            "proposal",
			TargetID:              fb.ProposalID,
			InvestigationID:       fb.SourceInvestigationID,
			ProposalID:            fb.ProposalID,
			Timestamp:             ts,
			Assessment:            fb.Assessment,
			UsefulHypothesisIDs:   append([]string(nil), fb.UsefulHypothesisIDs...),
			RejectedHypothesisIDs: append([]string(nil), fb.RejectedHypothesisIDs...),
			RationalePresent:      strings.TrimSpace(fb.Rationale) != "",
			RationaleRuneCount:    runeCount(fb.Rationale),
			LimitationCount:       len(fb.Limitations),
		}
		if err := attachFingerprint(&row.Envelope, row); err != nil {
			corrupt = append(corrupt, fmt.Sprintf("proposal feedback %s: %v", fb.FeedbackID, err))
			continue
		}
		rows = append(rows, row)
	}
	return rows, corrupt
}

func evalRows(rep eval.Report, exportedAt time.Time) (EvalRunRow, []EvalCaseResultRow, error) {
	if rep.SchemaVersion != eval.ReportSchemaVersion {
		return EvalRunRow{}, nil, fmt.Errorf("export: eval report schema_version %q unsupported", rep.SchemaVersion)
	}
	gen := rep.GeneratedAt.UTC()
	runID := evalRunID(rep.DatasetID, gen.Format(time.RFC3339Nano), rep.Mode)
	// Case rows carry evidence provenance; builtin fixtures are synthetic_fixture.
	// Unknown datasets stay unknown so they are not joined as observed_local.
	caseProv := ProvenanceUnknown
	if rep.DatasetID == eval.BuiltinDatasetID {
		caseProv = ProvenanceSyntheticFixture
	}
	run := EvalRunRow{
		Envelope:               envelope(DatasetEvalRuns, runID, rep.SchemaVersion, ProvenanceEvalAnnotation, exportedAt, &gen),
		EvalRunID:              runID,
		DatasetID:              rep.DatasetID,
		GeneratedAt:            gen,
		Mode:                   rep.Mode,
		CaseCount:              rep.Aggregate.CaseCount,
		DeterministicPassCount: rep.Aggregate.DeterministicPassCount,
		DeterministicDenom:     rep.Aggregate.DeterministicDenom,
		LLMAvailableCount:      rep.Aggregate.LLMAvailableCount,
		LLMPassCount:           rep.Aggregate.LLMPassCount,
		LLMPassDenom:           rep.Aggregate.LLMPassDenom,
		SyntheticProposalCount: rep.Aggregate.SyntheticProposalCount,
		RecordedProposalCount:  rep.Aggregate.RecordedProposalCount,
		MissingLLMCount:        rep.Aggregate.MissingLLMCount,
		HonestyLabelCount:      len(rep.Aggregate.HonestyLabels),
	}
	if err := attachFingerprint(&run.Envelope, run); err != nil {
		return EvalRunRow{}, nil, err
	}
	cases := append([]eval.CaseResult(nil), rep.Cases...)
	sort.SliceStable(cases, func(i, j int) bool { return cases[i].CaseID < cases[j].CaseID })
	var out []EvalCaseResultRow
	for _, c := range cases {
		key := runID + "|" + c.CaseID
		fbCount := 0
		if c.FeedbackSummary != nil {
			fbCount = c.FeedbackSummary.Count
		}
		row := EvalCaseResultRow{
			Envelope:                    envelope(DatasetEvalCaseResults, key, rep.SchemaVersion, caseProv, exportedAt, &gen),
			EvalRunID:                   runID,
			CaseID:                      c.CaseID,
			Title:                       c.Title,
			Tags:                        append([]string(nil), c.Tags...),
			IncidentID:                  c.IncidentID,
			InvestigationID:             c.InvestigationID,
			EvidenceFingerprint:         c.EvidenceFingerprint,
			DeterministicPass:           c.Deterministic.PassMechanical,
			DeterministicFalseCausal:    c.Deterministic.FalseCausalAttribution,
			DeterministicUnresolvedCite: c.Deterministic.UnresolvedCitationCount,
			LLMAvailable:                c.LLM.Available,
			LLMProposalOrigin:           c.LLM.ProposalOrigin,
			LLMPass:                     c.LLM.PassMechanical,
			LLMFalseCausal:              c.LLM.FalseCausalAttribution,
			LLMUnresolvedCite:           c.LLM.UnresolvedCitationCount,
			FeedbackCount:               fbCount,
		}
		if err := attachFingerprint(&row.Envelope, row); err != nil {
			return EvalRunRow{}, nil, err
		}
		out = append(out, row)
	}
	return run, out, nil
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
