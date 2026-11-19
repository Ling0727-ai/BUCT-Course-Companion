package portable

import (
	"bytes"
	"path/filepath"
	"testing"

	"buct-course-local/worker/internal/course"
)

func TestSyncRetainsHiddenItemsAndIncompleteKinds(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "course.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveAccount("sample-a", nil); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveTodo(course.Task{ID: "todo-1", Kind: "todo", Title: "personal"}); err != nil {
		t.Fatal(err)
	}
	tasks := []course.Task{{ID: "hw-1", Kind: "homework", Title: "Homework", Subject: "Class"},
		{ID: "test-1", Kind: "test", Title: "Quiz", Resource: &course.TaskResource{CourseID: "69", ItemID: "70"}}}
	if err = s.SaveSync(course.Result{Tasks: tasks}, 5); err != nil {
		t.Fatal(err)
	}
	if err = s.Hide(Filter{Kind: "course", Key: "id:69", Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSync(course.Result{IncompleteKinds: []string{"homework", "test"}}, 6); err != nil {
		t.Fatal(err)
	}
	visible, err := s.Tasks()
	if err != nil || len(visible) != 2 || s.Setting("last_sync", 0) != 5 {
		t.Fatalf("partial sync erased cache or visibility: %+v %v", visible, err)
	}
	if err = s.SaveAccount("sample-b", nil); err != nil {
		t.Fatal(err)
	}
	visible, err = s.Tasks()
	if err != nil || len(visible) != 1 || visible[0].Kind != "todo" {
		t.Fatalf("switching school account leaked course data: %+v %v", visible, err)
	}
}

func TestSavedScheduleRejectsAnOlderRevision(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "course.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state, err := s.Schedule()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSchedule(state); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSchedule(state); err == nil {
		t.Fatal("accepted an out-of-date schedule")
	}
}

func TestCredentialVaultRequiresTheProtectionPassphrase(t *testing.T) {
	account := Account{StudentID: "sample", Password: "fixture-only"}
	sealed, err := SealAccount(account, "a-test-passphrase")
	if err != nil || bytes.Contains(sealed, []byte(account.Password)) {
		t.Fatalf("credential was not sealed: %v", err)
	}
	if _, err = OpenAccount(sealed, "incorrect-passphrase"); err == nil {
		t.Fatal("vault opened with an incorrect passphrase")
	}
	stored, err := OpenAccount(sealed, "a-test-passphrase")
	if err != nil || stored != account {
		t.Fatalf("failed to unlock: %v", err)
	}
}
