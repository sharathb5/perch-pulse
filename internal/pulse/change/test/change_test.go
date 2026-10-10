package change_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/change"
)

func TestValidateAndSecretFields(t *testing.T) {
	if err := change.ValidateReportsSecretFieldNames(); err != nil {
		t.Fatal(err)
	}
	ev := change.MustRecord(change.RecordInput{
		Type:        change.ChangeDeployment,
		ServiceIDs:  []string{"astronomy/local/payment"},
		Environment: "local",
		CommitSHA:   "abc123",
		Simulated:   true,
		ObservedAt:  time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
	})
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
	if ev.SchemaVersion != change.SchemaVersion {
		t.Fatalf("schema %s", ev.SchemaVersion)
	}
	if ev.Metadata["simulated"] != "true" {
		t.Fatal("expected simulated metadata")
	}
}

func TestDuplicateAndConflict(t *testing.T) {
	store := change.NewMemory()
	ev := change.MustRecord(change.RecordInput{
		Type:       change.ChangeCommit,
		ServiceIDs: []string{"svc/a"},
		CommitSHA:  "deadbeef",
		ObservedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
	})
	if err := store.Append(ev); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ev); err != nil {
		t.Fatalf("idempotent duplicate: %v", err)
	}
	conflict := ev
	conflict.Title = "different"
	if err := store.Append(conflict); err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestOutOfOrderList(t *testing.T) {
	store := change.NewMemory()
	t1 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Minute)
	a := change.MustRecord(change.RecordInput{
		Type: change.ChangeDeployment, ServiceIDs: []string{"s/a"}, CommitSHA: "aa",
		ObservedAt: t2, CreatedAt: t2, DeployedAt: &t2,
	})
	b := change.MustRecord(change.RecordInput{
		Type: change.ChangeDeployment, ServiceIDs: []string{"s/b"}, CommitSHA: "bb",
		ObservedAt: t1, CreatedAt: t1, DeployedAt: &t1,
	})
	_ = store.Append(a)
	_ = store.Append(b)
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ChangeID != b.ChangeID {
		t.Fatalf("order by effective time: %+v", list)
	}
}

func TestServiceMapping(t *testing.T) {
	m := change.Mapper{Rules: []change.PathRule{
		{PathPrefix: "services/payment", ServiceID: "astronomy/local/payment"},
		{PathPrefix: "services/shipping", ServiceID: "astronomy/local/shipping"},
	}}
	ids, unc, _ := m.Resolve(nil, []string{"services/payment/main.go", "README.md"})
	if unc != change.MappingPartial || len(ids) != 1 || ids[0] != "astronomy/local/payment" {
		t.Fatalf("partial map: %v %s", ids, unc)
	}
	ids, unc, _ = m.Resolve([]string{"astronomy/local/frontend"}, []string{"services/payment/main.go"})
	if unc != change.MappingExplicit || ids[0] != "astronomy/local/frontend" {
		t.Fatalf("explicit wins: %v %s", ids, unc)
	}
	ids, unc, _ = m.Resolve(nil, nil)
	if unc != change.MappingUnknown || len(ids) != 0 {
		t.Fatalf("unknown: %v %s", ids, unc)
	}
}

func TestCausationRejected(t *testing.T) {
	_, err := change.Record(change.RecordInput{
		Type:       change.ChangeDeployment,
		ServiceIDs: []string{"s/a"},
		Summary:    "this deployment caused the outage",
		ObservedAt: time.Now().UTC(),
		CreatedAt:  time.Now().UTC(),
	})
	if err == nil || !strings.Contains(err.Error(), "causation") {
		t.Fatalf("want causation reject, got %v", err)
	}
}

func TestUnknownServicesRequireMapping(t *testing.T) {
	ev := change.Event{
		SchemaVersion:  change.SchemaVersion,
		ChangeID:       "x",
		ChangeType:     change.ChangeCommit,
		Source:         "test",
		CreatedAt:      time.Now().UTC(),
		ObservedAt:     time.Now().UTC(),
		ServiceMapping: change.MappingUnknown,
	}
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
}
