package decisions

import (
	"testing"
	"time"
)

func TestStore_AppendAndReadAll(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	_ = store.Append(Record{
		TaskID:    "t1",
		Question:  "Should we use Redis or Postgres for sessions?",
		Answer:    "Postgres",
		Reason:    "One fewer infra dependency",
		DecidedBy: "human",
		Timestamp: now,
	})
	_ = store.Append(Record{
		TaskID:    "t2",
		Question:  "REST or gRPC for internal APIs?",
		Answer:    "gRPC",
		Reason:    "Type safety and codegen",
		DecidedBy: "orchestrator",
		Timestamp: now,
	})

	all, err := store.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
	if all[0].Answer != "Postgres" {
		t.Errorf("expected Postgres, got %s", all[0].Answer)
	}
	if all[1].Question != "REST or gRPC for internal APIs?" {
		t.Errorf("unexpected question: %s", all[1].Question)
	}
}

func TestStore_Search(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	_ = store.Append(
		Record{TaskID: "t1", Question: "Use Redis?", Answer: "No", Reason: "complexity"},
		Record{TaskID: "t2", Question: "Use Postgres?", Answer: "Yes", Reason: "already in stack"},
		Record{TaskID: "t3", Question: "Auth library?", Answer: "custom", Reason: "control"},
	)

	results := store.Search("postgres")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'postgres', got %d", len(results))
	}

	results = store.Search("USE")
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'USE', got %d", len(results))
	}

	results = store.Search("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestStore_ReadAll_Empty(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)
	records, err := store.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}
