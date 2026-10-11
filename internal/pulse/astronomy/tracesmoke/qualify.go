package tracesmoke

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	traceIDRe = regexp.MustCompile(`^[a-fA-F0-9]{16}$|^[a-fA-F0-9]{32}$`)
	spanIDRe  = regexp.MustCompile(`^[a-fA-F0-9]{16}$`)
)

// Criteria selects a probe-produced Astronomy Shop trace.
type Criteria struct {
	// Workflow is a human label (e.g. RecommendationsBrowse).
	Workflow string
	// Marker must appear in user_agent / user_agent.original on at least one span.
	// This uses the demo's existing HTTP instrumentation — not custom baggage.
	Marker string
	// PathSubstring must appear in http.url or http.target on at least one span
	// when non-empty (e.g. "/api/recommendations").
	PathSubstring string
	// ProductID, when non-empty, must appear in http.url or a demo.product.* tag
	// on a span in the marked subgraph.
	ProductID string
	// WindowStart/WindowEnd bound span start times (inclusive). Zero means open.
	WindowStart time.Time
	WindowEnd   time.Time
	// MinAppServices is the minimum distinct ExpectedAppServices in the marked subgraph.
	// Defaults to 2 when zero.
	MinAppServices int
	// RequireCrossServiceEdge requires a CHILD_OF reference across two different
	// process service names within the marked subgraph when true.
	RequireCrossServiceEdge bool
	// AllowedServices, when non-nil, restricts which observed names are valid.
	// Defaults to KnownMappingServices.
	AllowedServices map[string]struct{}
}

// Result is a qualification decision for one trace.
type Result struct {
	OK                  bool
	TraceID             string
	Reason              string
	Services            []string
	AppServices         []string
	SpanCount           int
	SubgraphSpanCount   int
	EarliestStart       time.Time
	HasCrossServiceEdge bool
	MatchedMarker       bool
	MatchedPath         bool
	MatchedProduct      bool
}

// FindQualifyingTrace returns the first trace matching criteria.
// Empty data with valid JSON is "not found" (retryable). Malformed input must
// be rejected by ParseTraces before calling this.
func FindQualifyingTrace(traces []Trace, c Criteria) (Result, bool) {
	var last Result
	for _, tr := range traces {
		res := QualifyTrace(tr, c)
		last = res
		if res.OK {
			return res, true
		}
	}
	if len(traces) == 0 {
		return Result{OK: false, Reason: "no traces in Jaeger response"}, false
	}
	if last.Reason == "" {
		last.Reason = "no trace matched qualification criteria"
	}
	return last, false
}

// QualifyTrace evaluates a single Jaeger trace against criteria.
//
// Multi-service proof is restricted to the span subgraph reachable from spans
// that carry the probe User-Agent marker. Process table entries without spans
// in that subgraph do not count.
func QualifyTrace(tr Trace, c Criteria) Result {
	res := Result{
		TraceID:       tr.TraceID,
		SpanCount:     len(tr.Spans),
		EarliestStart: tr.EarliestStart(),
	}

	if !traceIDRe.MatchString(tr.TraceID) {
		res.Reason = "invalid or missing traceID"
		return res
	}
	if len(tr.Spans) == 0 {
		res.Reason = "missing spans"
		return res
	}
	if len(tr.Processes) == 0 {
		res.Reason = "missing process/service metadata"
		return res
	}

	seenSpanIDs := map[string]string{} // spanID -> processID
	for _, span := range tr.Spans {
		if span.TraceID == "" || span.TraceID != tr.TraceID {
			res.Reason = fmt.Sprintf("span %q missing or mismatched traceID", span.SpanID)
			return res
		}
		if !spanIDRe.MatchString(span.SpanID) {
			res.Reason = fmt.Sprintf("invalid spanID %q", span.SpanID)
			return res
		}
		if prev, ok := seenSpanIDs[span.SpanID]; ok && prev != span.ProcessID {
			res.Reason = fmt.Sprintf("duplicate spanID %q with conflicting process metadata", span.SpanID)
			return res
		}
		seenSpanIDs[span.SpanID] = span.ProcessID
		for _, ref := range span.References {
			if !strings.EqualFold(ref.RefType, "CHILD_OF") {
				continue
			}
			if ref.TraceID == "" || ref.TraceID != tr.TraceID {
				res.Reason = fmt.Sprintf("CHILD_OF on span %q missing or mismatched traceID", span.SpanID)
				return res
			}
			if !spanIDRe.MatchString(ref.SpanID) {
				res.Reason = fmt.Sprintf("invalid CHILD_OF spanID on %q", span.SpanID)
				return res
			}
		}
	}

	allowed := c.AllowedServices
	if allowed == nil {
		allowed = KnownMappingServices
	}

	marker := strings.TrimSpace(c.Marker)
	pathSub := strings.TrimSpace(c.PathSubstring)
	product := strings.TrimSpace(c.ProductID)

	markedRoots := map[string]struct{}{}
	for _, span := range tr.Spans {
		if marker != "" && spanHasMarker(span, marker) {
			res.MatchedMarker = true
			markedRoots[span.SpanID] = struct{}{}
		}
	}
	if marker != "" && !res.MatchedMarker {
		res.Reason = fmt.Sprintf("unrelated trace: marker %q not present in user-agent tags", marker)
		return res
	}

	// When no marker is requested (tests only), use the full span set.
	subgraph := map[string]struct{}{}
	if marker == "" {
		for _, span := range tr.Spans {
			subgraph[span.SpanID] = struct{}{}
		}
	} else {
		subgraph = descendantSubgraph(tr, markedRoots)
	}
	res.SubgraphSpanCount = len(subgraph)

	spanByID := map[string]Span{}
	spanSvc := map[string]string{}
	for _, span := range tr.Spans {
		spanByID[span.SpanID] = span
		p, ok := tr.Processes[span.ProcessID]
		if !ok || strings.TrimSpace(p.ServiceName) == "" {
			if _, in := subgraph[span.SpanID]; in {
				res.Reason = fmt.Sprintf("span %q missing process/service metadata", span.SpanID)
				return res
			}
			continue
		}
		spanSvc[span.SpanID] = strings.TrimSpace(p.ServiceName)
	}

	svcSet := map[string]struct{}{}
	for id := range subgraph {
		if name := spanSvc[id]; name != "" {
			svcSet[name] = struct{}{}
		}
	}
	res.Services = sortedKeys(svcSet)

	for _, name := range res.Services {
		if _, ok := allowed[name]; !ok {
			res.Reason = fmt.Sprintf("observed service %q is not in Astronomy Shop mapping allowlist", name)
			return res
		}
	}

	minApp := c.MinAppServices
	if minApp <= 0 {
		minApp = 2
	}
	appSet := map[string]struct{}{}
	for _, want := range ExpectedAppServices {
		appSet[want] = struct{}{}
	}
	var app []string
	for _, name := range res.Services {
		if _, ok := appSet[name]; ok {
			app = append(app, name)
		}
	}
	sort.Strings(app)
	res.AppServices = app

	if !c.WindowStart.IsZero() || !c.WindowEnd.IsZero() {
		earliest := earliestInSubgraph(tr, subgraph)
		res.EarliestStart = earliest
		if earliest.IsZero() {
			res.Reason = "trace outside test window: no usable span startTime in marked subgraph"
			return res
		}
		if !c.WindowStart.IsZero() && earliest.Before(c.WindowStart) {
			res.Reason = fmt.Sprintf("trace outside test window: earliest start %s before window start %s",
				earliest.Format(time.RFC3339Nano), c.WindowStart.UTC().Format(time.RFC3339Nano))
			return res
		}
		if !c.WindowEnd.IsZero() && earliest.After(c.WindowEnd) {
			res.Reason = fmt.Sprintf("trace outside test window: earliest start %s after window end %s",
				earliest.Format(time.RFC3339Nano), c.WindowEnd.UTC().Format(time.RFC3339Nano))
			return res
		}
	}

	for id := range subgraph {
		span := spanByID[id]
		url := spanURL(span)
		if pathSub != "" && strings.Contains(url, pathSub) {
			res.MatchedPath = true
		}
		if product != "" && spanHasProduct(span, product) {
			res.MatchedProduct = true
		}
	}
	if pathSub != "" && !res.MatchedPath {
		res.Reason = fmt.Sprintf("unrelated trace: path substring %q not present on marked subgraph", pathSub)
		return res
	}
	if product != "" && !res.MatchedProduct {
		res.Reason = fmt.Sprintf("unrelated trace: product id %q not present on marked subgraph", product)
		return res
	}

	if len(app) < minApp {
		res.Reason = fmt.Sprintf("single-service or insufficient app services in marked subgraph: have %v (need >= %d of %v)",
			app, minApp, ExpectedAppServices)
		return res
	}

	res.HasCrossServiceEdge = hasCrossServiceChildOfIn(subgraph, spanSvc, tr)
	if c.RequireCrossServiceEdge && !res.HasCrossServiceEdge {
		res.Reason = fmt.Sprintf("multi-service names present (%v) but no CHILD_OF edge across service boundaries in marked subgraph", app)
		return res
	}

	res.OK = true
	res.Reason = fmt.Sprintf("qualified %s multi-service trace services=%v spans=%d subgraph_spans=%d cross_service_edge=%t",
		c.Workflow, app, res.SpanCount, res.SubgraphSpanCount, res.HasCrossServiceEdge)
	return res
}

func spanHasMarker(span Span, marker string) bool {
	ua := TagString(span.Tags, "user_agent")
	if ua == "" {
		ua = TagString(span.Tags, "user_agent.original")
	}
	return strings.Contains(ua, marker)
}

func spanURL(span Span) string {
	url := TagString(span.Tags, "http.url")
	if url == "" {
		url = TagString(span.Tags, "http.target")
	}
	return url
}

func spanHasProduct(span Span, product string) bool {
	if strings.Contains(spanURL(span), product) {
		return true
	}
	if strings.Contains(TagString(span.Tags, "demo.product.id"), product) {
		return true
	}
	if strings.Contains(TagString(span.Tags, "demo.product.filtered.list"), product) {
		return true
	}
	return false
}

// descendantSubgraph returns marked roots plus spans that descend from them via CHILD_OF.
func descendantSubgraph(tr Trace, roots map[string]struct{}) map[string]struct{} {
	children := map[string][]string{}
	for _, span := range tr.Spans {
		for _, ref := range span.References {
			if !strings.EqualFold(ref.RefType, "CHILD_OF") {
				continue
			}
			children[ref.SpanID] = append(children[ref.SpanID], span.SpanID)
		}
	}
	out := map[string]struct{}{}
	var stack []string
	for id := range roots {
		stack = append(stack, id)
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, seen := out[id]; seen {
			continue
		}
		out[id] = struct{}{}
		stack = append(stack, children[id]...)
	}
	return out
}

func earliestInSubgraph(tr Trace, subgraph map[string]struct{}) time.Time {
	var min int64
	for _, span := range tr.Spans {
		if _, ok := subgraph[span.SpanID]; !ok {
			continue
		}
		if span.StartTime <= 0 {
			continue
		}
		if min == 0 || span.StartTime < min {
			min = span.StartTime
		}
	}
	if min == 0 {
		return time.Time{}
	}
	return time.UnixMicro(min).UTC()
}

func hasCrossServiceChildOfIn(subgraph map[string]struct{}, spanSvc map[string]string, tr Trace) bool {
	for _, span := range tr.Spans {
		if _, ok := subgraph[span.SpanID]; !ok {
			continue
		}
		childSvc := spanSvc[span.SpanID]
		if childSvc == "" {
			continue
		}
		for _, ref := range span.References {
			if !strings.EqualFold(ref.RefType, "CHILD_OF") {
				continue
			}
			if _, ok := subgraph[ref.SpanID]; !ok {
				continue
			}
			parentSvc := spanSvc[ref.SpanID]
			if parentSvc != "" && parentSvc != childSvc {
				return true
			}
		}
	}
	return false
}

func sortedKeys(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// SummaryLines returns bounded diagnostic lines for CI artifacts.
func (r Result) SummaryLines() []string {
	return []string{
		fmt.Sprintf("ok=%t", r.OK),
		fmt.Sprintf("trace_id=%s", r.TraceID),
		fmt.Sprintf("reason=%s", r.Reason),
		fmt.Sprintf("services=%s", strings.Join(r.Services, ",")),
		fmt.Sprintf("app_services=%s", strings.Join(r.AppServices, ",")),
		fmt.Sprintf("span_count=%d", r.SpanCount),
		fmt.Sprintf("subgraph_span_count=%d", r.SubgraphSpanCount),
		fmt.Sprintf("earliest_start=%s", formatTime(r.EarliestStart)),
		fmt.Sprintf("cross_service_edge=%t", r.HasCrossServiceEdge),
		fmt.Sprintf("matched_marker=%t", r.MatchedMarker),
		fmt.Sprintf("matched_path=%t", r.MatchedPath),
		fmt.Sprintf("matched_product=%t", r.MatchedProduct),
	}
}

// CompactTraceSummary is a redacted, bounded view of a Jaeger trace for artifacts.
// Only allowlisted fields are emitted (no raw tag dumps or full URLs).
func CompactTraceSummary(tr Trace, maxSpans int) string {
	if maxSpans <= 0 {
		maxSpans = 20
	}
	var b strings.Builder
	b.WriteString("trace_id=" + tr.TraceID + "\n")
	b.WriteString("services=" + strings.Join(sortedCopy(tr.ServiceNames()), ",") + "\n")
	b.WriteString(fmt.Sprintf("span_count=%d\n", len(tr.Spans)))
	n := 0
	for _, span := range tr.Spans {
		if n >= maxSpans {
			b.WriteString("… span summary truncated …\n")
			break
		}
		svc := ""
		if p, ok := tr.Processes[span.ProcessID]; ok {
			svc = p.ServiceName
		}
		ua := TagString(span.Tags, "user_agent")
		if ua == "" {
			ua = TagString(span.Tags, "user_agent.original")
		}
		markerPresent := strings.HasPrefix(ua, "perch-astronomy-smoke/")
		path := safePath(spanURL(span))
		op := safeOp(span.OperationName)
		b.WriteString(fmt.Sprintf("span service=%s op=%s start_us=%d marker=%t path=%s\n",
			svc, op, span.StartTime, markerPresent, path))
		n++
	}
	return b.String()
}

func safeOp(op string) string {
	op = strings.TrimSpace(op)
	if op == "" {
		return ""
	}
	// Keep short RPC/HTTP operation names; drop anything that looks like a payload.
	if len(op) > 80 {
		return op[:80] + "…"
	}
	lower := strings.ToLower(op)
	for _, key := range []string{"authorization", "password", "token", "secret"} {
		if strings.Contains(lower, key) {
			return "[redacted-op]"
		}
	}
	return op
}

func safePath(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "[redacted-path]"
	}
	// Relative URLs parse with Path empty and Opaque/raw in Path via RequestURI style.
	if u.Scheme == "" && u.Host == "" {
		u, err = url.Parse("http://local.invalid" + raw)
		if err != nil {
			return "[redacted-path]"
		}
	}
	productIDs := u.Query().Get("productIds")
	out := u.EscapedPath()
	if out == "" {
		out = u.Path
	}
	if productIDs != "" {
		out += "?productIds=" + url.QueryEscape(productIDs)
	}
	return out
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
