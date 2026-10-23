// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func refTransactionFixture(t *testing.T, packed bool) (string, string, NativeIdentity, []string) {
	t.Helper()
	d := fixtureGit(t)
	gitFixture(t, d, "branch", "target", "HEAD")
	gitFixture(t, d, "update-ref", "refs/stw/recovery/transaction", "HEAD")
	if packed {
		gitFixture(t, d, "pack-refs", "--all", "--prune")
	}
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	id, e := gitNativeIdentity(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	ro, e := gitTidyWriteProtection(id, []string{"refs/heads/target"})
	if e != nil {
		t.Fatal(e)
	}
	return d, bin, id, ro
}
func TestGitRefTransactionNativePreparedCommitAndAbort(t *testing.T) {
	for _, packed := range []bool{false, true} {
		for _, abort := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "loose", true: "packed"}[packed], map[bool]string{false: "commit", true: "abort"}[abort]}, "-"), func(t *testing.T) {
				d, bin, id, ro := refTransactionFixture(t, packed)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				tx, e := prepareGitRefDeletion(ctx, bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
				if e != nil {
					t.Fatalf("valid prepared native deletion refused: %v", e)
				}
				if _, e = os.Stat(filepath.Join(id.RepositoryDir, "refs", "heads", "target.lock")); e != nil {
					t.Fatal("prepared target lock missing", e)
				}
				if e = exec.Command(bin, "-C", d, "update-ref", "refs/heads/target", id.CommitID).Run(); e == nil {
					t.Fatal("cooperating writer bypassed prepared lock")
				}
				if abort {
					if e = tx.Abort(); e != nil {
						t.Fatal(e)
					}
					if gitFixture(t, d, "rev-parse", "refs/heads/target") != id.CommitID {
						t.Fatal("abort changed target")
					}
				} else {
					called := 0
					out, e := tx.Commit(ctx, func(context.Context) error { called++; return nil })
					if e != nil || !out.Prepared || !out.CommitAttempted || !out.Committed || out.Uncertain || called != 1 {
						t.Fatalf("commit outcome=%+v callback=%d error=%v", out, called, e)
					}
					if e = exec.Command(bin, "-C", d, "show-ref", "--verify", "--quiet", "refs/heads/target").Run(); e == nil {
						t.Fatal("committed target remains")
					}
				}
				if gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != id.CommitID || gitFixture(t, d, "rev-parse", "HEAD") != id.CommitID {
					t.Fatal("recovery/source altered")
				}
				if _, e = os.Stat(filepath.Join(id.RepositoryDir, "refs", "heads", "target.lock")); !os.IsNotExist(e) {
					t.Fatal("confirmed transaction left native lock")
				}
			})
		}
	}
}
func TestGitRefTransactionRefusesSymbolicAndChangedOID(t *testing.T) {
	for _, kind := range []string{"symbolic", "changed"} {
		t.Run(kind, func(t *testing.T) {
			d, bin, id, _ := refTransactionFixture(t, false)
			if kind == "symbolic" {
				gitFixture(t, d, "symbolic-ref", "refs/heads/target", id.HeadRef)
			} else {
				gitFixture(t, d, "commit", "--allow-empty", "-qm", "changed")
				gitFixture(t, d, "update-ref", "refs/heads/target", "HEAD")
			}
			ro, e := gitTidyWriteProtection(id, []string{"refs/heads/target"})
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tx, e := prepareGitRefDeletion(ctx, bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
			if e == nil {
				tx.Abort()
				t.Fatal("symbolic/changed target prepared as authority")
			}
			if gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != id.CommitID {
				t.Fatal("refusal changed recovery")
			}
			if kind == "symbolic" && gitFixture(t, d, "symbolic-ref", "refs/heads/target") != id.HeadRef {
				t.Fatal("symbolic referent/target changed")
			}
		})
	}
}
func TestGitRefTransactionRevalidationFailurePreservesRecovery(t *testing.T) {
	d, bin, id, ro := refTransactionFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, e := prepareGitRefDeletion(ctx, bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
	if e != nil {
		t.Fatal(e)
	}
	sentinel := errors.New("provider evidence changed")
	out, e := tx.Commit(ctx, func(context.Context) error { return sentinel })
	if !errors.Is(e, sentinel) || out.CommitAttempted || out.Committed {
		t.Fatalf("unsafe commit: %+v %v", out, e)
	}
	if gitFixture(t, d, "rev-parse", "refs/heads/target") != id.CommitID || gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != id.CommitID {
		t.Fatal("revalidation failure altered target/recovery")
	}
}

func TestGitRefTransactionLockedReadbackRejectsRawSwaps(t *testing.T) {
	for _, kind := range []string{"symbolic", "oid"} {
		t.Run(kind, func(t *testing.T) {
			d, bin, id, ro := refTransactionFixture(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tx, e := prepareGitRefDeletion(ctx, bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
			if e != nil {
				t.Fatal(e)
			}
			changed := "ref: " + id.HeadRef + "\n"
			if kind == "oid" {
				changed = strings.Repeat("b", 40) + "\n"
			}
			// Model a same-user filesystem writer bypassing native locks; cooperating
			// native writers were separately proven unable to change the prepared ref.
			if e = os.WriteFile(filepath.Join(id.RepositoryDir, "refs", "heads", "target"), []byte(changed), 0600); e != nil {
				t.Fatal(e)
			}
			called := false
			out, e := tx.Commit(ctx, func(context.Context) error { called = true; return nil })
			if e == nil || called || out.CommitAttempted || out.Uncertain {
				t.Fatalf("locked readback bypassed: %+v called=%v %v", out, called, e)
			}
			if gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != id.CommitID || gitFixture(t, d, "rev-parse", "HEAD") != id.CommitID {
				t.Fatal("raw swap refusal changed recovery/referent")
			}
			after, e := os.ReadFile(filepath.Join(id.RepositoryDir, "refs", "heads", "target"))
			if e != nil || string(after) != changed {
				t.Fatal("abort overwrote the actor's changed target")
			}

		})
	}
}
func TestGitRefTransactionCancellationAndLostCommitAck(t *testing.T) {
	for _, kind := range []string{"cancel", "lost-commit-ack"} {
		t.Run(kind, func(t *testing.T) {
			d, bin, id, ro := refTransactionFixture(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tx, e := prepareGitRefDeletion(ctx, bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "cancel" {
				cancel()
			}
			out, e := tx.Commit(context.Background(), func(context.Context) error {
				if kind == "lost-commit-ack" {
					tx.stdout.Close()
				}
				return nil
			})
			if e == nil || out.Committed || !out.Uncertain {
				t.Fatalf("uncertain effects misreported: %+v %v", out, e)
			}
			if kind == "lost-commit-ack" && !out.CommitAttempted {
				t.Fatal("commit attempt omitted")
			}
			if tx.cmd.ProcessState == nil {
				t.Fatal("cancelled transaction child not reaped")
			}
			if gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != id.CommitID {
				t.Fatal("uncertain transaction lost recovery")
			}
		})
	}
}
func TestGitRefTransactionAdmissionRefusesHelpersAndBroadGrants(t *testing.T) {
	for _, kind := range []string{"hook", "broad-grant"} {
		t.Run(kind, func(t *testing.T) {
			d, bin, id, ro := refTransactionFixture(t, false)
			sentinel := filepath.Join(gitTempDir(t), "hook-called")
			if kind == "hook" {
				if e := os.WriteFile(filepath.Join(id.RepositoryDir, "hooks", "reference-transaction"), []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0700); e != nil {
					t.Fatal(e)
				}
			} else {
				ro = nil
			}
			if tx, e := prepareGitRefDeletion(context.Background(), bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro); e == nil {
				tx.Abort()
				t.Fatal("unsafe admission passed")
			}
			if _, e := os.Stat(sentinel); !os.IsNotExist(e) {
				t.Fatal("helper executed")
			}
			if gitFixture(t, d, "rev-parse", "refs/heads/target") != id.CommitID {
				t.Fatal("admission refusal changed target")
			}
		})
	}
}

func TestGitRefTransactionNativeOldOIDCompareAndSwap(t *testing.T) {
	d, bin, id, _ := refTransactionFixture(t, false)
	old := id.CommitID
	gitFixture(t, d, "commit", "--allow-empty", "-qm", "second native commit")
	gitFixture(t, d, "update-ref", "refs/heads/target", "HEAD")
	id, e := gitNativeIdentity(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	ro, e := gitTidyWriteProtection(id, []string{"refs/heads/target"})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, e := prepareGitRefDeletion(ctx, bin, id, "refs/heads/target", old, []string{id.RepositoryDir}, ro)
	if e == nil {
		tx.Abort()
		t.Fatal("stale old OID prepared")
	}
	if gitFixture(t, d, "rev-parse", "refs/heads/target") != id.CommitID || gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != old {
		t.Fatal("stale CAS changed target/recovery")
	}
	if tx != nil && tx.cmd.ProcessState == nil {
		t.Fatal("failed prepare was not reaped")
	}
}
func TestGitRefTransactionCommitRetainsTighterCallerDeadline(t *testing.T) {
	d, bin, id, ro := refTransactionFixture(t, false)
	parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, e := prepareGitRefDeletion(parent, bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
	if e != nil {
		t.Fatal(e)
	}
	ctx, stop := context.WithTimeout(parent, 30*time.Millisecond)
	defer stop()
	out, e := tx.Commit(ctx, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	if !errors.Is(e, context.DeadlineExceeded) || out.CommitAttempted || out.Committed || parent.Err() != nil {
		t.Fatalf("deadline authority reset: %+v %v parent=%v", out, e, parent.Err())
	}
	if gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != id.CommitID {
		t.Fatal("deadline lost recovery")
	}
}

type refDelayCommitAckReader struct {
	source   io.Reader
	ctx      context.Context
	observed chan struct{}
}

func (r *refDelayCommitAckReader) Read(p []byte) (int, error) {
	n, e := r.source.Read(p)
	if n == 1 && p[0] == '\n' {
		close(r.observed)
		<-r.ctx.Done()
	}
	return n, e
}
func TestGitRefTransactionPostAckFailureRetainsDeadlineCause(t *testing.T) {
	d, bin, id, ro := refTransactionFixture(t, false)
	parent, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	tx, e := prepareGitRefDeletion(parent, bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
	if e != nil {
		t.Fatal(e)
	}
	caller, cancel := context.WithTimeout(parent, 100*time.Millisecond)
	defer cancel()
	observed := make(chan struct{})
	tx.output.r = &refDelayCommitAckReader{source: tx.output.r, ctx: caller, observed: observed}
	out, e := tx.Commit(caller, func(context.Context) error { return nil })
	select {
	case <-observed:
	default:
		t.Fatal("test did not reach the post-ack boundary")
	}
	if !errors.Is(e, context.DeadlineExceeded) || !out.CommitAttempted || out.Committed || !out.Uncertain || parent.Err() != nil {
		t.Fatalf("post-ack deadline lost: %+v error=%v parent=%v", out, e, parent.Err())
	}
	if gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != id.CommitID {
		t.Fatal("post-ack failure lost recovery")
	}
}

// The locked readback also compares object names, so the stale-OID test above
// cannot tell whether Git itself was asked to compare-and-swap. Pin the command
// bytes, and show that native Git refuses a stale value from exactly them.
func TestGitRefDeleteCommandPinsOldOIDCompareAndSwap(t *testing.T) {
	d, bin, id, _ := refTransactionFixture(t, false)
	old := id.CommitID
	if got, want := gitRefDeleteCommand("refs/heads/target", old), "option no-deref\x00delete refs/heads/target\x00"+old+"\x00prepare\x00"; got != want {
		t.Fatalf("prepared delete command = %q, want %q", got, want)
	}
	gitFixture(t, d, "commit", "--allow-empty", "-qm", "second native commit")
	gitFixture(t, d, "update-ref", "refs/heads/target", "HEAD")
	moved := gitFixture(t, d, "rev-parse", "refs/heads/target")
	cmd := exec.Command(bin, "--no-replace-objects", "update-ref", "--stdin", "-z")
	cmd.Dir = d
	cmd.Stdin = strings.NewReader("start\x00" + gitRefDeleteCommand("refs/heads/target", old))
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("stale old OID accepted by native Git: %q", out)
	}
	if gitFixture(t, d, "rev-parse", "refs/heads/target") != moved {
		t.Fatal("stale compare-and-swap changed the target")
	}
}

// A cancelled refusal must not depend on whether the cancel watcher or a clean
// abort acknowledgment wins: it is uncertain and the child is killed and reaped.
func TestGitRefTransactionRefuseAfterCancellationIsUncertain(t *testing.T) {
	d, bin, id, ro := refTransactionFixture(t, false)
	tx, e := prepareGitRefDeletion(context.Background(), bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the watcher goroutine has not run: the transaction itself is not cancelled
	out, e := tx.refuse(ctx, errors.New("cancelled after intent"))
	if e == nil || !out.Prepared || out.CommitAttempted || out.Committed || !out.Uncertain {
		t.Fatalf("cancelled refusal not uncertain: %+v %v", out, e)
	}
	if !tx.stopped || tx.cmd.ProcessState == nil {
		t.Fatal("cancelled refusal did not reap the child")
	}
	if gitFixture(t, d, "rev-parse", "refs/stw/recovery/transaction") != id.CommitID {
		t.Fatal("cancelled refusal lost recovery")
	}
}

// Commit with a cancelled caller context while the prepare context stays live
// must not be a confirmed abort.
func TestGitRefTransactionCommitCancelledCallerIsUncertain(t *testing.T) {
	_, bin, id, ro := refTransactionFixture(t, false)
	tx, e := prepareGitRefDeletion(context.Background(), bin, id, "refs/heads/target", id.CommitID, []string{id.RepositoryDir}, ro)
	if e != nil {
		t.Fatal(e)
	}
	caller, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	out, e := tx.Commit(caller, func(context.Context) error { called = true; return nil })
	if e == nil || called || !out.Prepared || out.CommitAttempted || out.Committed || !out.Uncertain {
		t.Fatalf("cancelled caller not uncertain: %+v called=%v %v", out, called, e)
	}
	if !tx.stopped || tx.cmd.ProcessState == nil {
		t.Fatal("child not reaped")
	}
}
