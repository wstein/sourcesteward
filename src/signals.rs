//! SIGINT and SIGTERM for `stw watch` (D-20261119-04). This file may name libc for signals.
//! It does not start a process. The process scan allows libc here and nowhere else except
//! `git_runner.rs`.
use std::sync::atomic::{AtomicBool, Ordering};

static WATCH_STOP: AtomicBool = AtomicBool::new(false);

extern "C" fn handle_watch_stop(_: libc::c_int) {
    WATCH_STOP.store(true, Ordering::Relaxed);
}

/// Arms SIGINT and SIGTERM so `watch_stopped` becomes true, and clears a stop left by an earlier stream.
pub fn arm_watch_stop() -> Result<(), String> {
    WATCH_STOP.store(false, Ordering::SeqCst);
    unsafe {
        let mut action: libc::sigaction = std::mem::zeroed();
        action.sa_sigaction = handle_watch_stop as *const () as libc::sighandler_t;
        action.sa_flags = libc::SA_RESTART;
        if libc::sigemptyset(&mut action.sa_mask) != 0 {
            return Err("cannot clear the signal mask".to_string());
        }
        for sig in [libc::SIGINT, libc::SIGTERM] {
            if libc::sigaction(sig, &action, std::ptr::null_mut()) != 0 {
                return Err("cannot install the stop handler".to_string());
            }
        }
    }
    Ok(())
}

pub fn watch_stopped() -> bool {
    WATCH_STOP.load(Ordering::Relaxed)
}
