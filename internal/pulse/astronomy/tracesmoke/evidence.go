package tracesmoke

import (
	"sort"
	"strings"
	"time"
)

const (
	defaultMaxSpans    = 40
	defaultMaxEdges    = 24
	defaultMaxServices = 32
	maxOpLen           = 80
)

// Evidence is a bounded, redacted, observed-only view of a qualified trace.
// It does not invent critical-path or bottleneck inferences.
type Evidence struct {
	TraceID             string    `json:"trace_id"`
	SpanCount           int       `json:"span_count"`
	SubgraphSpanCount   int       `json:"subgraph_span_count"`
	HasCrossServiceEdge bool      `json:"has_cross_service_edge"`
	EarliestStart       time.Time `json:"earliest_start,omitempty"`
	LatestEnd           time.Time `json:"latest_end,omitempty"`
	// TraceDurationMs is latest_end - earliest_start across the marked subgraph.
	// Separately labeled from server-measured request elapsed time.
	TraceDurationMs *float64 `json:"trace_duration_ms,omitempty"`
	// ObservedServices are process service names seen in the marked subgraph.
	ObservedServices []string `json:"observed_services"`
	// AppServices are ExpectedAppServices present in ObservedServices.
	AppServices []string `json:"app_services"`
	// Services hold per-service observed span timing (not overlapping-sum e2e).
	Services []ServiceEvidence `json:"services"`
	// Spans are bounded span rows from the marked subgraph.
	Spans []SpanEvidence `json:"spans"`
	// CrossServiceEdges are verified CHILD_OF edges across service boundaries.
	CrossServiceEdges []CrossServiceEdge `json:"cross_service_edges"`
	// Warnings carry missing/unknown evidence notes (never success claims).
	Warnings []string `json:"warnings,omitempty"`
}

// ServiceEvidence aggregates observed spans for one service identity.
type ServiceEvidence struct {
	OTELServiceName string `json:"otel_service_name"`
	SpanCount       int    `json:"span_count"`
	// LongestSpanDurationMs is the duration of the single longest observed span.
	LongestSpanDurationMs float64 `json:"longest_span_duration_ms"`
	// MinSpanDurationMs / MaxSpanDurationMs bound individual span durations.
	MinSpanDurationMs float64 `json:"min_span_duration_ms"`
	MaxSpanDurationMs float64 `json:"max_span_duration_ms"`
	ErrorSpanCount    int     `json:"error_span_count"`
	HasError          bool    `json:"has_error"`
}

// SpanEvidence is one redacted span row.
type SpanEvidence struct {
	SpanID          string  `json:"span_id"`
	ParentSpanID    string  `json:"parent_span_id,omitempty"`
	OTELServiceName string  `json:"otel_service_name"`
	Operation       string  `json:"operation"`
	DurationMs      float64 `json:"duration_ms"`
	StartUnixMicro  int64   `json:"start_unix_micro"`
	HasError        bool    `json:"has_error"`
	ErrorKind       string  `json:"error_kind,omitempty"`
	SafePath        string  `json:"path,omitempty"`
}

// CrossServiceEdge is a verified parent→child relationship across services.
type CrossServiceEdge struct {
	ParentService string `json:"parent_service"`
	ChildService  string `json:"child_service"`
	ParentSpanID  string `json:"parent_span_id"`
	ChildSpanID   string `json:"child_span_id"`
}

// BuildEvidence extracts observed evidence from a trace using a qualification result.
// Prefer calling with a Result that already identified the marked subgraph criteria.
func BuildEvidence(tr Trace, qual Result, c Criteria) Evidence {
	ev := Evidence{
		TraceID:             tr.TraceID,
		SpanCount:           len(tr.Spans),
		HasCrossServiceEdge: qual.HasCrossServiceEdge,
		ObservedServices:    append([]string(nil), qual.Services...),
		AppServices:         append([]string(nil), qual.AppServices...),
		SubgraphSpanCount:   qual.SubgraphSpanCount,
	}

	marker := strings.TrimSpace(c.Marker)
	markedRoots := map[string]struct{}{}
	for _, span := range tr.Spans {
		if marker == "" || spanHasMarker(span, marker) {
			if marker == "" {
				markedRoots[span.SpanID] = struct{}{}
			} else if spanHasMarker(span, marker) {
				markedRoots[span.SpanID] = struct{}{}
			}
		}
	}
	var subgraph map[string]struct{}
	if marker == "" {
		subgraph = map[string]struct{}{}
		for _, span := range tr.Spans {
			subgraph[span.SpanID] = struct{}{}
		}
	} else {
		subgraph = descendantSubgraph(tr, markedRoots)
	}
	ev.SubgraphSpanCount = len(subgraph)

	spanByID := map[string]Span{}
	spanSvc := map[string]string{}
	for _, span := range tr.Spans {
		spanByID[span.SpanID] = span
		if p, ok := tr.Processes[span.ProcessID]; ok {
			spanSvc[span.SpanID] = strings.TrimSpace(p.ServiceName)
		}
	}

	type agg struct {
		count    int
		errCount int
		minMs    float64
		maxMs    float64
		seen     bool
	}
	bySvc := map[string]*agg{}
	var earliest, latest int64
	var spans []SpanEvidence
	var edges []CrossServiceEdge
	edgeSeen := map[string]struct{}{}

	for _, span := range tr.Spans {
		if _, ok := subgraph[span.SpanID]; !ok {
			continue
		}
		svc := spanSvc[span.SpanID]
		if span.Duration < 0 {
			ev.Warnings = append(ev.Warnings, "omitted span with negative duration")
			continue
		}
		durMs := float64(span.Duration) / 1000.0
		errKind, hasErr := spanError(span)

		if span.StartTime > 0 {
			if earliest == 0 || span.StartTime < earliest {
				earliest = span.StartTime
			}
			end := span.StartTime + span.Duration
			if end > span.StartTime && end > latest { // guard overflow / zero
				latest = end
			}
		}

		a := bySvc[svc]
		if a == nil {
			a = &agg{}
			bySvc[svc] = a
		}
		a.count++
		if hasErr {
			a.errCount++
		}
		if !a.seen {
			a.minMs = durMs
			a.maxMs = durMs
			a.seen = true
		} else {
			if durMs < a.minMs {
				a.minMs = durMs
			}
			if durMs > a.maxMs {
				a.maxMs = durMs
			}
		}

		parentID := ""
		for _, ref := range span.References {
			if strings.EqualFold(ref.RefType, "CHILD_OF") {
				parentID = ref.SpanID
				break
			}
		}

		if len(spans) < defaultMaxSpans {
			spans = append(spans, SpanEvidence{
				SpanID:          span.SpanID,
				ParentSpanID:    parentID,
				OTELServiceName: svc,
				Operation:       safeOp(span.OperationName),
				DurationMs:      durMs,
				StartUnixMicro:  span.StartTime,
				HasError:        hasErr,
				ErrorKind:       errKind,
				SafePath:        safePath(spanURL(span)),
			})
		}

		if parentID != "" {
			if _, ok := subgraph[parentID]; ok {
				ps := spanSvc[parentID]
				cs := svc
				if ps != "" && cs != "" && ps != cs {
					key := ps + "->" + cs + "|" + parentID + "|" + span.SpanID
					if _, dup := edgeSeen[key]; !dup && len(edges) < defaultMaxEdges {
						edgeSeen[key] = struct{}{}
						edges = append(edges, CrossServiceEdge{
							ParentService: ps,
							ChildService:  cs,
							ParentSpanID:  parentID,
							ChildSpanID:   span.SpanID,
						})
					}
				}
			}
		}
	}

	if earliest > 0 {
		ev.EarliestStart = time.UnixMicro(earliest).UTC()
	}
	if latest > 0 {
		ev.LatestEnd = time.UnixMicro(latest).UTC()
	}
	if earliest > 0 && latest >= earliest {
		ms := float64(latest-earliest) / 1000.0
		ev.TraceDurationMs = &ms
	}

	svcNames := make([]string, 0, len(bySvc))
	for name := range bySvc {
		if name != "" {
			svcNames = append(svcNames, name)
		}
	}
	sort.Strings(svcNames)
	if len(svcNames) > defaultMaxServices {
		ev.Warnings = append(ev.Warnings, "service summary truncated")
		svcNames = svcNames[:defaultMaxServices]
	}
	ev.ObservedServices = svcNames
	for _, name := range svcNames {
		a := bySvc[name]
		ev.Services = append(ev.Services, ServiceEvidence{
			OTELServiceName:       name,
			SpanCount:             a.count,
			LongestSpanDurationMs: a.maxMs,
			MinSpanDurationMs:     a.minMs,
			MaxSpanDurationMs:     a.maxMs,
			ErrorSpanCount:        a.errCount,
			HasError:              a.errCount > 0,
		})
	}

	// Stable span order by start time then span id.
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].StartUnixMicro != spans[j].StartUnixMicro {
			return spans[i].StartUnixMicro < spans[j].StartUnixMicro
		}
		return spans[i].SpanID < spans[j].SpanID
	})
	ev.Spans = spans
	ev.CrossServiceEdges = edges
	ev.HasCrossServiceEdge = len(edges) > 0 || qual.HasCrossServiceEdge

	if len(subgraph) == 0 {
		ev.Warnings = append(ev.Warnings, "marked subgraph has no spans — missing evidence")
	}
	if len(ev.AppServices) < 2 {
		ev.Warnings = append(ev.Warnings, "fewer than two expected application services observed in marked subgraph")
	}
	if !ev.HasCrossServiceEdge {
		ev.Warnings = append(ev.Warnings, "no verified cross-service CHILD_OF edge in marked subgraph")
	}
	if qual.SpanCount > 0 && ev.SubgraphSpanCount < qual.SpanCount {
		// Informational — not a failure.
	}
	if len(tr.Spans) > defaultMaxSpans && len(spans) >= defaultMaxSpans {
		ev.Warnings = append(ev.Warnings, "span list truncated for response size bounds")
	}

	return ev
}

func spanError(span Span) (kind string, has bool) {
	if TagString(span.Tags, "error") == "true" {
		return "error_tag", true
	}
	code := strings.ToUpper(TagString(span.Tags, "otel.status_code"))
	if code == "" {
		code = strings.ToUpper(TagString(span.Tags, "status.code"))
	}
	if code == "ERROR" || code == "STATUS_CODE_ERROR" || code == "2" {
		return "status_error", true
	}
	httpStatus := TagString(span.Tags, "http.status_code")
	if httpStatus == "" {
		httpStatus = TagString(span.Tags, "http.response.status_code")
	}
	if len(httpStatus) == 3 && (httpStatus[0] == '5' || httpStatus[0] == '4') {
		// 4xx/5xx on span is observed failure evidence for that span.
		if httpStatus[0] == '5' {
			return "http_5xx", true
		}
		return "http_4xx", true
	}
	return "", false
}
