//! `stw status --stats` and `--verbose`: timing and API usage of the run. Diagnostic only: counts, durations and
//! the rate-limit numbers GitHub reported; never a token and never a URL.
use std::time::Duration;

use serde_json::{json, Value};

use crate::github::Stats;

/// How long the phases of the run took.
#[derive(Clone, Copy, Debug, Default)]
pub struct Timings {
    pub total: Duration,
    pub local: Duration,
    pub remote: Duration,
}

/// A duration for people: `850ms` below a second, `1.4s` below ten, `38s` below a minute, `2m05s` above.
pub fn short_duration(d: Duration) -> String {
    if d < Duration::from_secs(1) {
        format!("{}ms", d.as_millis())
    } else if d < Duration::from_secs(10) {
        format!("{:.1}s", d.as_secs_f64())
    } else if d < Duration::from_secs(60) {
        format!("{}s", d.as_secs())
    } else {
        let s = d.as_secs();
        format!("{}m{:02}s", s / 60, s % 60)
    }
}

/// `4892` as `4,892`.
pub fn group_digits(n: u64) -> String {
    let s = n.to_string();
    let mut out = String::new();
    for (i, c) in s.chars().enumerate() {
        if i > 0 && (s.len() - i).is_multiple_of(3) {
            out.push(',');
        }
        out.push(c);
    }
    out
}

/// Unix time as `(YYYY-MM-DD, HH:MM:SS)` in UTC.
fn utc(t: u64) -> (String, String) {
    let (days, rem) = ((t / 86_400) as i64, t % 86_400);
    // days since 1970-01-01 to a civil date (Howard Hinnant's algorithm)
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = yoe + era * 400 + i64::from(m <= 2);
    (
        format!("{y:04}-{m:02}-{d:02}"),
        format!("{:02}:{:02}:{:02}", rem / 3600, rem % 3600 / 60, rem % 60),
    )
}

/// The `stats` member of the JSON document.
pub fn to_json(stats: &Stats, t: &Timings) -> Value {
    let rate = stats.rate;
    json!({
        "requests": stats.requests,
        "rate_limit_limit": rate.map(|r| r.limit),
        "rate_limit_remaining": rate.map(|r| r.remaining),
        "rate_limit_reset": rate.map(|r| { let (d, h) = utc(r.reset); format!("{d}T{h}Z") }),
        "duration_ms": t.total.as_millis() as u64,
    })
}

/// `stats: API: 18 reqs (4,892/5,000 left, reset 22:00 UTC) | 1.4s elapsed (local 110ms, remote 1.3s)`
pub fn stats_line(stats: &Stats, t: &Timings) -> String {
    let mut s = format!(
        "stats: API: {} {}",
        stats.requests,
        plural(stats.requests, "req", "reqs")
    );
    if let Some(r) = stats.rate {
        let (_, h) = utc(r.reset);
        s += &format!(
            " ({}/{} left, reset {} UTC)",
            group_digits(r.remaining),
            group_digits(r.limit),
            &h[..5]
        );
    }
    s += &format!(
        " | {} elapsed (local {}, remote {})",
        short_duration(t.total),
        short_duration(t.local),
        short_duration(t.remote)
    );
    s
}

fn plural(n: usize, one: &str, many: &str) -> String {
    if n == 1 { one } else { many }.to_string()
}

/// The `--verbose` breakdown per repository and source, ending with a newline. `esc` shows text from outside
/// (the repository slug) with its control characters escaped.
pub fn breakdown(stats: &Stats, esc: impl Fn(&str) -> String) -> String {
    let mut out = String::from("Diagnostic breakdown:\n");
    let mut last: Option<&str> = None;
    for s in stats.sources.iter().filter(|s| !s.repo.is_empty()) {
        if last != Some(s.repo.as_str()) {
            out += &format!("  {}:\n", esc(&s.repo));
            last = Some(&s.repo);
        }
        // no ETag cache: a request is never answered 304, every request is consumed
        out += &format!(
            "    {:<11} {} {} (0 cached 304s, {} consumed) in {}\n",
            format!("{}:", s.source),
            s.requests,
            plural(s.requests, "req", "reqs"),
            s.requests,
            short_duration(s.latency)
        );
    }
    for s in stats.sources.iter().filter(|s| s.repo.is_empty()) {
        out += &format!(
            "  other:      {} {} (0 cached 304s, {} consumed) in {}\n",
            s.requests,
            plural(s.requests, "req", "reqs"),
            s.requests,
            short_duration(s.latency)
        );
    }
    out
}
