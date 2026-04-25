package task

import "testing"

func TestStoreDeleteRemovesTask(t *testing.T) {
	store := NewStore(t.TempDir())
	task := Task{ID: "task_test", Title: "test"}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(task.ID); err == nil {
		t.Fatalf("expected deleted task load to fail")
	}
}
