package line

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoPushGuard walks every .go file in this package directory and fails if
// any source file references LINE proactive-send methods.  This is a CI guard
// that enforces the hard rule: LINE OA must only reply via replyToken; push,
// multicast, and broadcast are contractually forbidden by our Terms of Service
// with LINE and would incur costs without user consent.
func TestNoPushGuard(t *testing.T) {
	forbidden := []string{
		"PushMessage",
		"MulticastMessage",
		"BroadcastMessage",
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}

	var violations []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		// Skip this guard test file itself — it intentionally contains the
		// forbidden strings as string literals inside the test.
		if entry.Name() == "no_push_guard_test.go" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		src := stripGoComments(string(content))
		for _, term := range forbidden {
			if strings.Contains(src, term) {
				violations = append(violations, path+": "+term)
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("LINE OA must not push, multicast, or broadcast — found %d violation(s):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

// stripGoComments removes block comments (/* ... */) and line comments (// ...)
// so the guard cannot be defeated by hiding forbidden terms in comments.
func stripGoComments(src string) string {
	// Remove block comments.
	for {
		start := strings.Index(src, "/*")
		if start < 0 {
			break
		}
		end := strings.Index(src[start:], "*/")
		if end < 0 {
			break
		}
		src = src[:start] + src[start+end+2:]
	}

	// Remove line comments.
	var lines []string
	for _, line := range strings.Split(src, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
