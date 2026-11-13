//! The op log readers: `refs.log` and `rewrites.log` of `<git-common-dir>/stw/oplog/` and the reflogs, as `stw oplog
//! timeline | evolog | undo-plan` (read-only; format and rules: `docs/oplog-format.md`). Writing logs, rotation, backup
//! refs, the undo itself and the hook install are later packages.
use std::path::{Path, PathBuf};

use crate::error::{Error, Result};

pub mod evolog;
pub mod parse;
pub mod store;
pub mod timeline;
pub mod undo_plan;
pub mod write;

/// The repository of a command: the common directory (shared by all linked worktrees) and, when gix can open it, the
/// repository handle. A repository whose ref backend gix does not read has a common directory and no handle.
pub struct Ctx {
    pub repo: Option<gix::Repository>,
    pub common: PathBuf,
}

impl Ctx {
    pub fn open(path: &Path) -> Result<Ctx> {
        match crate::repo::open(path) {
            Ok(repo) => {
                let common = repo.common_dir().to_path_buf();
                Ok(Ctx {
                    repo: Some(repo),
                    common,
                })
            }
            Err(e) => {
                let dot = path.join(".git");
                if dot.is_dir() {
                    Ok(Ctx {
                        repo: None,
                        common: dot,
                    })
                } else {
                    Err(Error::repository(format!(
                        "cannot open the repository: {e}"
                    )))
                }
            }
        }
    }

    pub fn repo(&self) -> Result<&gix::Repository> {
        self.repo.as_ref().ok_or_else(|| {
            Error::repository(
                "this repository cannot be opened (extensions.refStorage other than files?); the op log needs the files backend",
            )
        })
    }
}

/// A value of the repository's own `config` file (no includes, no other config), `None` when absent.
pub fn config_value(common: &Path, key: &str) -> Option<String> {
    let f =
        gix::config::File::from_path_no_includes(common.join("config"), gix::config::Source::Local)
            .ok()?;
    f.string(key).map(|v| v.to_string())
}
