package tracesmoke_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy/tracesmoke"
)

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	p := filepath.Join(filepath.Dir(file), "..", "testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func baseCriteria() tracesmoke.Criteria {
	return tracesmoke.Criteria{
		Workflow:                tracesmoke.RecommendationsBrowse,
		Marker:                  "perch-astronomy-smoke/run-test-marker",
		PathSubstring:           tracesmoke.RecommendationsPath,
		ProductID:               tracesmoke.DefaultProductID,
		WindowStart:             time.UnixMicro(1700000000000000).UTC(),
		WindowEnd:               time.UnixMicro(1700000002000000).UTC(),
		MinAppServices:          2,
		RequireCrossServiceEdge: true,
	}
}

func TestParseAndQualify_ValidMultiService(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_multi_service.json"))
	if err != nil {
		t.Fatalf("ParseTraces: %v", err)
	}
	res, ok := tracesmoke.FindQualifyingTrace(resp.Data, baseCriteria())
	if !ok || !res.OK {
		t.Fatalf("expected qualify ok, got ok=%v res=%+v", ok, res)
	}
	if res.TraceID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("trace id = %q", res.TraceID)
	}
	if !res.HasCrossServiceEdge {
		t.Fatal("expected cross-service CHILD_OF edge")
	}
	if len(res.AppServices) < 2 {
		t.Fatalf("app services = %v", res.AppServices)
	}
}

func TestQualify_SingleServiceFails(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_single_service.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := tracesmoke.FindQualifyingTrace(resp.Data, baseCriteria())
	if ok || res.OK {
		t.Fatalf("single-service must fail: %+v", res)
	}
	if !contains(res.Reason, "insufficient app services") && !contains(res.Reason, "single-service") {
		t.Fatalf("reason = %q", res.Reason)
	}
}

func TestQualify_UnrelatedBackgroundTrafficFails(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_unrelated_trace.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := tracesmoke.FindQualifyingTrace(resp.Data, baseCriteria())
	if ok || res.OK {
		t.Fatalf("unrelated traffic must fail: %+v", res)
	}
	if !contains(res.Reason, "marker") && !contains(res.Reason, "path") {
		t.Fatalf("reason should cite marker/path, got %q", res.Reason)
	}
}

func TestQualify_OutsideWindowFails(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_outside_window.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := tracesmoke.FindQualifyingTrace(resp.Data, baseCriteria())
	if ok || res.OK {
		t.Fatalf("outside window must fail: %+v", res)
	}
	if !contains(res.Reason, "outside test window") {
		t.Fatalf("reason = %q", res.Reason)
	}
}

func TestParse_MalformedFailsImmediately(t *testing.T) {
	_, err := tracesmoke.ParseTraces(testdata(t, "synthetic_malformed.json"))
	if err == nil {
		t.Fatal("expected malformed parse error")
	}
	if !contains(err.Error(), "malformed") {
		t.Fatalf("err = %v", err)
	}
}

func TestQualify_MissingSpansFails(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_missing_spans.json"))
	if err != nil {
		t.Fatal(err)
	}
	res := tracesmoke.QualifyTrace(resp.Data[0], baseCriteria())
	if res.OK || !contains(res.Reason, "missing spans") {
		t.Fatalf("got %+v", res)
	}
}

func TestQualify_MissingProcessesFails(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_missing_processes.json"))
	if err != nil {
		t.Fatal(err)
	}
	res := tracesmoke.QualifyTrace(resp.Data[0], baseCriteria())
	if res.OK || !contains(res.Reason, "missing process") {
		t.Fatalf("got %+v", res)
	}
}

func TestQualify_DuplicateSpansStillOK(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_duplicate_spans.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := tracesmoke.FindQualifyingTrace(resp.Data, baseCriteria())
	if !ok || !res.OK {
		t.Fatalf("duplicate spans should still qualify: %+v", res)
	}
}

func TestQualify_ValidServiceMapping(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_multi_service.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range resp.Data[0].ServiceNames() {
		if _, ok := tracesmoke.KnownMappingServices[name]; !ok {
			t.Fatalf("fixture service %q missing from KnownMappingServices", name)
		}
	}
}

func TestQualify_InvalidServiceMappingFails(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_invalid_service_mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := tracesmoke.FindQualifyingTrace(resp.Data, baseCriteria())
	if ok || res.OK {
		t.Fatalf("invalid mapping must fail: %+v", res)
	}
	if !contains(res.Reason, "allowlist") {
		t.Fatalf("reason = %q", res.Reason)
	}
}

func TestFind_EmptyTracesNotFound(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_empty_traces.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := tracesmoke.FindQualifyingTrace(resp.Data, baseCriteria())
	if ok || res.OK {
		t.Fatalf("empty traces must not qualify: %+v", res)
	}
	if !contains(res.Reason, "no traces") {
		t.Fatalf("reason = %q", res.Reason)
	}
}

func TestQualify_DisconnectedMultiServiceFails(t *testing.T) {
	resp, err := tracesmoke.ParseTraces(testdata(t, "synthetic_disconnected_multiservice.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := tracesmoke.FindQualifyingTrace(resp.Data, baseCriteria())
	if ok || res.OK {
		t.Fatalf("disconnected multi-service must fail: %+v", res)
	}
	if !contains(res.Reason, "marked subgraph") && !contains(res.Reason, "insufficient app services") {
		t.Fatalf("reason = %q", res.Reason)
	}
	if len(res.AppServices) >= 2 {
		t.Fatalf("app services should be from marked subgraph only, got %v", res.AppServices)
	}
}

func TestParseServices_OK(t *testing.T) {
	resp, err := tracesmoke.ParseServices(testdata(t, "synthetic_services.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) < 2 {
		t.Fatalf("services = %v", resp.Data)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
