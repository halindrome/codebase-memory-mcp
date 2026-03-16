package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// pragmaVal queries a single PRAGMA and returns its value as a string.
func pragmaVal(t *testing.T, s *Store, pragma string) string {
	t.Helper()
	var val string
	if err := s.DB().QueryRowContext(context.Background(), "PRAGMA "+pragma).Scan(&val); err != nil {
		t.Fatalf("PRAGMA %s: %v", pragma, err)
	}
	return val
}

// TestBulkWriteKeepsWAL asserts that BeginBulkWrite does not exit WAL mode and
// that EndBulkWrite restores synchronous = NORMAL (1).
func TestBulkWriteKeepsWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	defer s.Close()

	if err := s.UpsertProject("test", t.TempDir()); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	ctx := context.Background()

	// Baseline: WAL mode must be active after open.
	if got := pragmaVal(t, s, "journal_mode"); got != "wal" {
		t.Errorf("baseline journal_mode = %q, want \"wal\"", got)
	}

	s.BeginBulkWrite(ctx)

	// After BeginBulkWrite: journal mode must still be WAL (fix regression check).
	if got := pragmaVal(t, s, "journal_mode"); got != "wal" {
		t.Errorf("during bulk write: journal_mode = %q, want \"wal\"", got)
	}
	// After BeginBulkWrite: synchronous must be OFF (0).
	if got := pragmaVal(t, s, "synchronous"); got != "0" {
		t.Errorf("during bulk write: synchronous = %q, want \"0\" (OFF)", got)
	}

	s.EndBulkWrite(ctx)

	// After EndBulkWrite: journal mode still WAL.
	if got := pragmaVal(t, s, "journal_mode"); got != "wal" {
		t.Errorf("after EndBulkWrite: journal_mode = %q, want \"wal\"", got)
	}
	// After EndBulkWrite: synchronous must be NORMAL (1).
	if got := pragmaVal(t, s, "synchronous"); got != "1" {
		t.Errorf("after EndBulkWrite: synchronous = %q, want \"1\" (NORMAL)", got)
	}
}

// TestCrashHelper is the subprocess body for TestBulkWriteCrashRecovery.
// When CRASH_HELPER_DB is set, it opens the DB, calls BeginBulkWrite, inserts a
// row, and exits via os.Exit(1) — simulating a SIGKILL mid-bulk-write.
// When the env var is absent the test is a no-op (running as part of the normal suite).
func TestCrashHelper(t *testing.T) {
	dbPath := os.Getenv("CRASH_HELPER_DB")
	if dbPath == "" {
		return // not running as subprocess
	}
	s, err := OpenPath(dbPath)
	if err != nil {
		os.Exit(2)
	}
	ctx := context.Background()
	_ = s.UpsertProject("crash-test", dbPath)
	s.BeginBulkWrite(ctx)
	// Insert a node to create write activity before the simulated crash.
	_, _ = s.UpsertNode(&Node{
		Project:       "crash-test",
		Label:         "Function",
		Name:          "CrashFunc",
		QualifiedName: "crash.CrashFunc",
		FilePath:      "crash.go",
		StartLine:     1,
		EndLine:       5,
	})
	// Simulate crash: exit without calling EndBulkWrite.
	os.Exit(1)
}

// TestBulkWriteCrashRecovery forks a subprocess that opens a real file-backed DB,
// calls BeginBulkWrite, inserts a row, then exits via os.Exit(1). The parent then
// reopens the DB and verifies that PRAGMA integrity_check returns "ok".
func TestBulkWriteCrashRecovery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crash.db")

	// Pre-create the DB so the schema exists before the subprocess opens it.
	s, err := OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath (pre-create): %v", err)
	}
	s.Close()

	// Fork subprocess that crashes mid-bulk-write.
	cmd := exec.Command(os.Args[0], "-test.run=TestCrashHelper", "-test.v")
	cmd.Env = append(os.Environ(), "CRASH_HELPER_DB="+dbPath)
	out, err := cmd.CombinedOutput()
	t.Logf("subprocess output: %s", out)
	// The subprocess must have exited with code 1 (simulated crash via os.Exit(1)).
	// Any other outcome — clean exit (0), setup failure (2), or launch error —
	// means the crash was never exercised and the rest of the test is meaningless.
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("subprocess did not crash as expected: err=%v, output=%s", err, out)
	}

	// Reopen the DB — must not error.
	s2, err := OpenPath(dbPath)
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer s2.Close()

	// Run integrity check — must return "ok".
	ctx := context.Background()
	var result string
	if err := s2.DB().QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatalf("integrity_check query: %v", err)
	}
	if result != "ok" {
		t.Errorf("DB corrupted after crash: integrity_check = %q, want \"ok\"", result)
	}

	// Check whether the row inserted by the subprocess survived the crash.
	// With synchronous = OFF, SQLite delegates durability to the OS page cache.
	// On a true power failure the row may be lost; on a normal process-kill the
	// OS typically flushes the cache and the row survives. Either outcome is
	// acceptable — this is an intentional trade-off of the bulk-write mode.
	// We log the result so test runs surface the actual behavior without failing.
	var rowCount int
	_ = s2.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM nodes WHERE qualified_name = 'crash.CrashFunc'",
	).Scan(&rowCount)
	t.Logf("row survived crash: %v (synchronous=OFF means this may legitimately be false on power-loss)", rowCount > 0)
}
