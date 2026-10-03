package cli_test

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordor-forge/lamplight/v2/internal/cli"
)

// The advice study topic remove prints, followed to the letter, brings the
// Topic back under its id, in a Study home whose path needs quoting.
func TestTopicRemoveAdviceRestoresTheTopic(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Ada's study home")
	if r := run(t, home, "topic", "create", "--title", "Linear algebra", "--goal", "Solve systems by hand"); r.code != cli.ExitOK {
		t.Fatalf("topic create: %s", r.stderr)
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"topic", "remove", "linear-algebra"}, strings.NewReader(""),
		&stdout, &stderr, options(home, home)); code != cli.ExitOK {
		t.Fatalf("topic remove: exit %d, %s", code, stderr.String())
	}
	var advice string
	for _, line := range strings.Split(stdout.String(), "\n") {
		// The command is the one indented line.
		if strings.HasPrefix(line, "  ") {
			advice = strings.TrimSpace(line)
		}
	}
	if advice == "" {
		t.Fatalf("no restore command in:\n%s", stdout.String())
	}
	if r := run(t, home, "status"); !strings.Contains(r.stdout, "No Topics yet") {
		t.Fatalf("status after the removal:\n%s", r.stdout)
	}
	if out, err := exec.Command("sh", "-c", advice).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", advice, err, out)
	}
	r := run(t, home, "topic", "update", "linear-algebra", "--title", "Linear algebra", "--json")
	if r.code != cli.ExitOK || !strings.Contains(r.stdout, `"goal": "Solve systems by hand"`) {
		t.Errorf("the restored Topic: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}
