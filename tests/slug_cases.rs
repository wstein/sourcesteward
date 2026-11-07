//! Data-driven: every `tests/slug_cases/<name>.url` holds an origin URL, the sibling `<name>.slug` the
//! expected `owner/repo` or `none`. Add a case by adding two files.
mod common;
use std::path::Path;

use common::*;

fn check(url_file: &Path) -> datatest_stable::Result<()> {
    let url = std::fs::read_to_string(url_file)?;
    let want = std::fs::read_to_string(url_file.with_extension("slug"))?;
    let fx = Fx::new();
    fx.set_origin_url(url.trim());
    let got = stw::repo::slug(&fx.repo()).unwrap_or_else(|| "none".into());
    assert_eq!(got, want.trim(), "{}", url.trim());
    Ok(())
}

datatest_stable::harness! {
    { test = check, root = "tests/slug_cases", pattern = r"\.url$" },
}
