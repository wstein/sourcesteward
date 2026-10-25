// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// refTransactionWithDescendant starts a fake owned writer: a parent process that
// starts a descendant sharing its stderr, as bwrap's git does, and reports
// ready. Closing release lets the descendant exit; alive reads EOF once it has.
func refTransactionWithDescendant(t *testing.T, mode string, descendantArgs ...string) (tx *gitRefTransaction, release func(), alive *os.File) {
	t.Helper()
	old := nativeUnsandboxed
	nativeUnsandboxed = true
	t.Cleanup(func() { nativeUnsandboxed = old })
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if bin, err = filepath.EvalSymlinks(bin); err != nil {
		t.Fatal(err)
	}
	root := gitTempDir(t)
	runCtx, cancel := context.WithCancel(context.Background())
	cmd, err := prepareSandboxed(runCtx, bin, append([]string{"-test.run=^TestNativeSandboxHelper$", "--", "stw-sandbox-helper", mode}, descendantArgs...), root, nil, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	aliveRead, aliveWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.ExtraFiles = []*os.File{controlRead, aliveWrite}
	tx, err = startGitRefTransaction(context.Background(), runCtx, cancel, cmd, bin, NativeIdentity{}, "refs/heads/x", "")
	controlRead.Close()
	aliveWrite.Close()
	if err != nil {
		t.Fatal(err)
	}
	var once bool
	release = func() {
		if !once {
			once = true
			controlWrite.Close()
		}
	}
	t.Cleanup(func() { release(); aliveRead.Close(); tx.cancel() })
	if err := tx.ack("ready\n"); err != nil {
		t.Fatal("fake writer did not start its descendant:", err)
	}
	return tx, release, aliveRead
}

// descendantAlive checks without waiting for the descendant: it closes alive
// before it exits, so end of file is already there when finish waited. The
// short deadline only matters when the descendant is alive (an expired
// deadline would fail even a readable pipe).
func descendantAlive(t *testing.T, alive *os.File) bool {
	t.Helper()
	if err := alive.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err := alive.Read(make([]byte, 1))
	return !errors.Is(err, io.EOF)
}

func descendantGone(t *testing.T, alive *os.File) bool {
	t.Helper()
	if err := alive.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err := alive.Read(make([]byte, 1))
	return errors.Is(err, io.EOF)
}

func TestRefTransactionKillReportsSurvivingDescendantHonestly(t *testing.T) {
	old := gitRefDescendantWait
	gitRefDescendantWait = 100 * time.Millisecond
	t.Cleanup(func() { gitRefDescendantWait = old })
	tx, release, alive := refTransactionWithDescendant(t, "spawn")
	err := tx.finish(true)
	if !errors.Is(err, errGitRefDescendants) {
		t.Fatalf("killed writer reported as fully stopped while its descendant runs: %v", err)
	}
	if !tx.out.Uncertain {
		t.Fatal("surviving descendant did not make the outcome uncertain")
	}
	if !tx.stopped || tx.cmd.ProcessState == nil {
		t.Fatal("owned process was not reaped")
	}
	if again := tx.finish(true); !errors.Is(again, errGitRefDescendants) {
		t.Fatalf("repeated finish lost the descendant result: %v", again)
	}
	release()
	if !descendantGone(t, alive) {
		t.Fatal("descendant did not exit after release")
	}
}

func TestRefTransactionCleanStopReportsSurvivingDescendantHonestly(t *testing.T) {
	old := gitRefDescendantWait
	gitRefDescendantWait = 100 * time.Millisecond
	t.Cleanup(func() { gitRefDescendantWait = old })
	tx, release, _ := refTransactionWithDescendant(t, "spawn-exit")
	err := tx.finish(false)
	if !errors.Is(err, errGitRefDescendants) || !tx.out.Uncertain {
		t.Fatalf("clean finish hid a surviving descendant: %v uncertain=%v", err, tx.out.Uncertain)
	}
	release()
}

func TestRefTransactionStderrFloodDoesNotSkipTheDescendantWait(t *testing.T) {
	old := gitRefDescendantWait
	gitRefDescendantWait = 100 * time.Millisecond
	t.Cleanup(func() { gitRefDescendantWait = old })
	tx, release, _ := refTransactionWithDescendant(t, "spawn-flood")
	// The capture hitting its limit cancels the run, which closes stdout: wait for
	// that, so that the flood is certainly over before finish is called.
	if n, err := tx.output.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("unexpected output after the flood: %d %v", n, err)
	}
	err := tx.finish(true)
	if !errors.Is(err, errNativeOutputLimit) {
		t.Fatalf("flood past the stderr limit was not reported: %v", err)
	}
	if !errors.Is(err, errGitRefDescendants) || !tx.out.Uncertain {
		t.Fatalf("stderr limit skipped the descendant wait: %v", err)
	}
	release()
}

func TestRefTransactionDescendantWaitEndsWithTheContext(t *testing.T) {
	old := gitRefDescendantWait
	gitRefDescendantWait = time.Minute
	t.Cleanup(func() { gitRefDescendantWait = old })
	tx, release, _ := refTransactionWithDescendant(t, "spawn")
	ctx, cancel := context.WithCancel(context.Background())
	tx.ctx = ctx
	cancel()
	err := tx.finish(true)
	if !errors.Is(err, errGitRefDescendants) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context did not end the descendant wait honestly: %v", err)
	}
	release()
}

func TestRefTransactionKillWaitsForDescendantThatIsGone(t *testing.T) {
	tx, release, alive := refTransactionWithDescendant(t, "spawn")
	release()
	err := tx.finish(true)
	if errors.Is(err, errGitRefDescendants) {
		t.Fatalf("exited descendant reported as running: %v", err)
	}
	if descendantAlive(t, alive) {
		t.Fatal("finish returned while the descendant was still alive")
	}
}

// The interleaving of issue 196: a fixture restores a lock once the transaction
// reports its writer stopped, and the writer's descendant then unlinks that
// lock path (git's rollback on stdin end of file). A transaction that reports a
// clean stop must have no such descendant left; one that cannot say so must
// not look clean, so the caller keeps its hands off the path.
func TestRefTransactionCleanStopLeavesNoDescendantToUnlinkARestoredLock(t *testing.T) {
	old := gitRefDescendantWait
	gitRefDescendantWait = 100 * time.Millisecond
	t.Cleanup(func() { gitRefDescendantWait = old })
	lock := filepath.Join(t.TempDir(), "integrated.lock")
	tx, release, alive := refTransactionWithDescendant(t, "spawn", lock)
	err := tx.finish(true)
	clean := !errors.Is(err, errGitRefDescendants)
	if clean {
		if err := os.WriteFile(lock, []byte("restored\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if !tx.out.Uncertain {
		t.Fatal("a stop with a possible descendant must be uncertain")
	}
	release()
	if !descendantGone(t, alive) {
		t.Fatal("descendant did not exit after release")
	}
	if clean {
		if _, err := os.Lstat(lock); err != nil {
			t.Fatal("a stop reported as clean let a descendant remove the restored lock:", err)
		}
	}
}
