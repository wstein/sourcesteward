//! The reader of `stw watch` (port of the Go observer, package W2): conditional REST GETs with ETag, Link paging,
//! rate-limit rules and error classes (`http`), the store of validators (`cond_store`), the observation types of the core
//! (`obs`) and the observer that reads only what the accepted state makes necessary (`host`).
//! Read-only: every request is a GET; there is no write path. The token never reaches an output, an error text or an
//! observation.
pub mod http;
