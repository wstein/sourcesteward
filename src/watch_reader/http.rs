//! Pure rules of the watch reader: what the reader may request, how headers are read and what the rate limit asks for.

/// Rate-limit state from the `X-RateLimit-*` headers or from `rate_limit`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct RateInfo {
    pub known: bool,
    pub limit: u64,
    pub remaining: u64,
    /// Unix time of the reset.
    pub reset: i64,
}

/// What the rate limit asks of the caller.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Advice {
    None,
    Slow,
    Wait,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RateDecision {
    pub advice: Advice,
    /// Unix time: reset plus 5 s for `Wait`, 0 otherwise.
    pub until: i64,
}

pub fn assess_rate_limit(_info: &RateInfo) -> RateDecision {
    RateDecision {
        advice: Advice::None,
        until: 0,
    }
}

pub fn rate_limited_until(_retry_after: u64, _rate: &RateInfo, now: i64) -> i64 {
    now
}

pub fn parse_retry_after(_header: &str) -> Option<u64> {
    None
}

pub fn parse_rate_headers(_limit: &str, _remaining: &str, _reset: &str) -> RateInfo {
    RateInfo::default()
}

pub fn next_page_from_link(_link: &str) -> Option<u64> {
    None
}

pub fn read_endpoint_allowed(_endpoint: &str) -> bool {
    false
}

pub fn encode_branch_segment(_name: &str) -> Option<String> {
    None
}

pub fn branch_segment_valid(_raw: &str) -> bool {
    false
}

pub fn valid_repo(_s: &str) -> bool {
    false
}

pub fn valid_sha(_s: &str) -> bool {
    false
}

pub fn valid_etag(_s: &str) -> bool {
    false
}

pub fn split_remote(_remote: &str) -> Option<(String, String)> {
    None
}

pub fn parse_remote(_remote: &str, _host: &str) -> Option<String> {
    None
}

pub fn valid_host(_s: &str) -> bool {
    false
}
