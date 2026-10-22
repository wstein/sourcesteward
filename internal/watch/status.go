// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Status is the `--status --json` object (contracts/watch-events.md). It is read from the state
// directory only: no network, no lock, nothing written.
type Status struct {
	SchemaVersion   int              `json:"schema_version"`
	StateDir        string           `json:"state_dir"`
	Heartbeat       *StatusHeartbeat `json:"heartbeat"`
	Fresh           bool             `json:"fresh"`
	IntervalSeconds int              `json:"interval_seconds"`
	Seq             int64            `json:"seq"`
	LockHeld        bool             `json:"lock_held"`
	Repos           []StatusRepo     `json:"repos"`
	LockPID         int              `json:"-"`
}

// StatusHeartbeat is the last completed cycle.
type StatusHeartbeat struct {
	Time       string `json:"time"`
	AgeSeconds int    `json:"age_seconds"`
	PID        int    `json:"pid"`
	Mode       string `json:"-"`
}

// StatusRepo is one repository of the watched set.
type StatusRepo struct {
	Repo       string  `json:"repo"`
	LastOK     *string `json:"last_ok"`
	AgeSeconds *int    `json:"age_seconds"`
	Failures   int     `json:"failures"`
	OpenError  *string `json:"open_error"`
}

// StatusFreshness is the heartbeat age below which a watcher counts as alive: max(3 x interval, 3 min).
func StatusFreshness(interval time.Duration) time.Duration {
	if d := 3 * interval; d > 3*time.Minute {
		return d
	}
	return 3 * time.Minute
}

// ReadStatus reads the liveness of the watcher that owns dir. A missing directory or heartbeat is not an
// error: the result is simply not fresh. A corrupt state.json is ErrCorruptState.
func ReadStatus(dir string, now time.Time) (Status, error) {
	s := Status{SchemaVersion: 1, StateDir: dir, IntervalSeconds: 60, Repos: []StatusRepo{}}
	if !filepath.IsAbs(dir) {
		return s, ErrStateDirUsage
	}
	st, err := loadState(dir)
	if err != nil {
		return s, err
	}
	if st != nil {
		if st.IntervalSeconds > 0 {
			s.IntervalSeconds = st.IntervalSeconds
		}
		for _, r := range st.Repos {
			sr := StatusRepo{Repo: r}
			if rs := st.Repo[r]; rs != nil && rs.LastOK != "" {
				if t, err := time.Parse(time.RFC3339, rs.LastOK); err == nil {
					age := ageSeconds(now, t)
					sr.LastOK, sr.AgeSeconds = &rs.LastOK, &age
				}
			}
			if h := st.Health[r]; h != nil {
				sr.Failures = h.Failures
				if h.ErrOpen && h.LastCode != "" {
					code := h.LastCode
					sr.OpenError = &code
				}
			}
			s.Repos = append(s.Repos, sr)
		}
	}
	if f, err := os.Open(filepath.Join(dir, journalName)); err == nil {
		events, _, serr := scanJournal(f)
		f.Close()
		if serr != nil {
			return s, fmt.Errorf("%w: %v", ErrCorruptState, serr)
		}
		if n := len(events); n > 0 {
			s.Seq = events[n-1].Seq
		}
	}
	s.LockHeld, s.LockPID = lockHolder(dir)
	if hb, err := readHeartbeat(dir, now); err == nil {
		s.Heartbeat = hb
		s.Fresh = time.Duration(hb.AgeSeconds)*time.Second < StatusFreshness(time.Duration(s.IntervalSeconds)*time.Second)
	} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errBadHeartbeat) {
		return s, err
	}
	return s, nil
}

var errBadHeartbeat = errors.New("heartbeat is unreadable")

func ageSeconds(now, t time.Time) int {
	if d := now.Sub(t); d > 0 {
		return int(d / time.Second)
	}
	return 0
}

func readHeartbeat(dir string, now time.Time) (*StatusHeartbeat, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "heartbeat"))
	if err != nil {
		return nil, err
	}
	f := strings.Fields(string(raw))
	if len(f) != 3 {
		return nil, errBadHeartbeat
	}
	t, err := time.Parse(time.RFC3339, f[0])
	pid, perr := strconv.Atoi(f[1])
	if err != nil || perr != nil {
		return nil, errBadHeartbeat
	}
	return &StatusHeartbeat{Time: f[0], AgeSeconds: ageSeconds(now, t), PID: pid, Mode: f[2]}, nil
}
