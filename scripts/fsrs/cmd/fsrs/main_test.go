package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// buildBinary compiles the fsrs binary into a temp directory and returns its path.
func buildBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fsrs")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, out)
	}
	return binary
}

// runFSRS runs the binary with the given args and FSRS_STORE env var.
// Returns stdout, stderr, and exit code.
func runFSRS(t *testing.T, binary, storePath string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "FSRS_STORE="+storePath)

	var stdout, stderr []byte
	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start binary: %v", err)
	}

	stdout, _ = readAll(stdoutPipe)
	stderr, _ = readAll(stderrPipe)

	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	return string(stdout), string(stderr), exitCode
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var result []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return result, nil
}

func TestCLINoArgs(t *testing.T) {
	binary := buildBinary(t)
	_, stderr, code := runFSRS(t, binary, filepath.Join(t.TempDir(), "cards.json"))
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if len(stderr) == 0 {
		t.Error("expected usage message on stderr")
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	binary := buildBinary(t)
	_, stderr, code := runFSRS(t, binary, filepath.Join(t.TempDir(), "cards.json"), "bogus")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if len(stderr) == 0 {
		t.Error("expected error message on stderr")
	}
}

func TestCLIStatusEmpty(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	stdout, _, code := runFSRS(t, binary, storePath, "status")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	var result struct {
		Total int `json:"total"`
		Due   int `json:"due"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, stdout)
	}
	if result.Total != 0 {
		t.Errorf("expected 0 total, got %d", result.Total)
	}
}

func TestCLIAddCard(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "store", "cards.json")

	stdout, _, code := runFSRS(t, binary, storePath, "add", "lesson-01", "Goroutines", "1")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	var card struct {
		ID       string `json:"id"`
		Topic    string `json:"topic"`
		LessonNum int   `json:"lesson_num"`
	}
	if err := json.Unmarshal([]byte(stdout), &card); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, stdout)
	}
	if card.ID != "lesson-01" {
		t.Errorf("expected id 'lesson-01', got %q", card.ID)
	}
	if card.Topic != "Goroutines" {
		t.Errorf("expected topic 'Goroutines', got %q", card.Topic)
	}
	if card.LessonNum != 1 {
		t.Errorf("expected lesson_num 1, got %d", card.LessonNum)
	}

	// Verify file was created
	if _, err := os.Stat(storePath); err != nil {
		t.Errorf("store file not created: %v", err)
	}
}

func TestCLIAddDuplicate(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	// Add first card
	_, _, code := runFSRS(t, binary, storePath, "add", "lesson-01", "Goroutines", "1")
	if code != 0 {
		t.Fatalf("first add failed with exit code %d", code)
	}

	// Add duplicate
	_, stderr, code := runFSRS(t, binary, storePath, "add", "lesson-01", "Goroutines", "1")
	if code != 1 {
		t.Errorf("expected exit code 1 for duplicate, got %d", code)
	}
	if len(stderr) == 0 {
		t.Error("expected error message for duplicate")
	}
}

func TestCLIScheduleOutput(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	// Add a card (new cards are immediately due)
	runFSRS(t, binary, storePath, "add", "lesson-01", "Goroutines", "1")

	stdout, _, code := runFSRS(t, binary, storePath, "schedule")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// Verify wrapped format with due_count
	var result struct {
		DueCount int               `json:"due_count"`
		Cards    []json.RawMessage `json:"cards"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse schedule JSON: %v\noutput: %s", err, stdout)
	}
	if result.DueCount != 1 {
		t.Errorf("expected due_count=1, got %d", result.DueCount)
	}
	if len(result.Cards) != 1 {
		t.Errorf("expected 1 card, got %d", len(result.Cards))
	}
}

func TestCLIReviewCard(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	// Add a card
	runFSRS(t, binary, storePath, "add", "lesson-01", "Goroutines", "1")

	// Review it with rating=3 (Good)
	stdout, _, code := runFSRS(t, binary, storePath, "review", "lesson-01", "3")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	var card struct {
		ID      string `json:"id"`
		Reviews int    `json:"reviews"`
		State   int    `json:"state"`
	}
	if err := json.Unmarshal([]byte(stdout), &card); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, stdout)
	}
	if card.Reviews != 1 {
		t.Errorf("expected reviews=1, got %d", card.Reviews)
	}
	if card.State == 0 { // should not be New anymore
		t.Error("card should not be in New state after review")
	}
}

func TestCLIReviewInvalidRating(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	runFSRS(t, binary, storePath, "add", "lesson-01", "Test", "1")

	// Rating 0 (invalid)
	_, stderr, code := runFSRS(t, binary, storePath, "review", "lesson-01", "0")
	if code != 1 {
		t.Errorf("expected exit code 1 for invalid rating, got %d", code)
	}
	if len(stderr) == 0 {
		t.Error("expected error message for invalid rating")
	}

	// Rating 5 (invalid)
	_, stderr, code = runFSRS(t, binary, storePath, "review", "lesson-01", "5")
	if code != 1 {
		t.Errorf("expected exit code 1 for invalid rating, got %d", code)
	}
	if len(stderr) == 0 {
		t.Error("expected error message for invalid rating")
	}
}

func TestCLIReviewNonexistentCard(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	_, stderr, code := runFSRS(t, binary, storePath, "review", "nonexistent", "3")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if len(stderr) == 0 {
		t.Error("expected error message for nonexistent card")
	}
}

func TestCLIFullWorkflow(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	// 1. Status should be empty
	stdout, _, _ := runFSRS(t, binary, storePath, "status")
	var status struct {
		Total int `json:"total"`
	}
	json.Unmarshal([]byte(stdout), &status)
	if status.Total != 0 {
		t.Fatalf("expected 0 cards initially, got %d", status.Total)
	}

	// 2. Add 3 cards
	runFSRS(t, binary, storePath, "add", "lesson-01", "Goroutines", "1")
	runFSRS(t, binary, storePath, "add", "lesson-02", "Channels", "2")
	runFSRS(t, binary, storePath, "add", "lesson-03", "Select", "3")

	// 3. Status should show 3 cards
	stdout, _, _ = runFSRS(t, binary, storePath, "status")
	json.Unmarshal([]byte(stdout), &status)
	if status.Total != 3 {
		t.Fatalf("expected 3 cards, got %d", status.Total)
	}

	// 4. Schedule should show all 3 as due (new cards)
	stdout, _, _ = runFSRS(t, binary, storePath, "schedule")
	var schedule struct {
		DueCount int `json:"due_count"`
	}
	json.Unmarshal([]byte(stdout), &schedule)
	if schedule.DueCount != 3 {
		t.Fatalf("expected 3 due, got %d", schedule.DueCount)
	}

	// 5. Review lesson-01 with Good (3)
	stdout, _, code := runFSRS(t, binary, storePath, "review", "lesson-01", "3")
	if code != 0 {
		t.Fatalf("review failed with exit code %d", code)
	}
	var reviewed struct {
		Due string `json:"due"`
	}
	json.Unmarshal([]byte(stdout), &reviewed)
	if reviewed.Due == "" {
		t.Error("reviewed card should have a due date")
	}

	// 6. Status should still show 3 total but fewer new
	stdout, _, _ = runFSRS(t, binary, storePath, "status")
	var finalStatus struct {
		Total int `json:"total"`
		New   int `json:"new"`
	}
	json.Unmarshal([]byte(stdout), &finalStatus)
	if finalStatus.Total != 3 {
		t.Errorf("expected 3 total, got %d", finalStatus.Total)
	}
	if finalStatus.New != 2 {
		t.Errorf("expected 2 new (one reviewed), got %d", finalStatus.New)
	}
}

func TestCLIWrappedStoreFormat(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	// Write a wrapped format file (what an LLM might create)
	os.WriteFile(storePath, []byte(`{"cards":[]}`), 0o644)

	// Add should work with the wrapped format
	stdout, _, code := runFSRS(t, binary, storePath, "add", "test-1", "Test", "1")
	if code != 0 {
		t.Fatalf("add failed with wrapped store format, exit code %d", code)
	}

	var card struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(stdout), &card)
	if card.ID != "test-1" {
		t.Errorf("expected id 'test-1', got %q", card.ID)
	}
}

func TestCLIAddMissingArgs(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	// Missing lesson_num
	_, _, code := runFSRS(t, binary, storePath, "add", "id", "topic")
	if code != 1 {
		t.Errorf("expected exit code 1 for missing args, got %d", code)
	}

	// Missing everything
	_, _, code = runFSRS(t, binary, storePath, "add")
	if code != 1 {
		t.Errorf("expected exit code 1 for missing args, got %d", code)
	}
}

func TestCLIReviewMissingArgs(t *testing.T) {
	binary := buildBinary(t)
	storePath := filepath.Join(t.TempDir(), "cards.json")

	// Missing rating
	_, _, code := runFSRS(t, binary, storePath, "review", "id")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	// Missing everything
	_, _, code = runFSRS(t, binary, storePath, "review")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}
