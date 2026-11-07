//! Developer helper behind the hidden `stw dev patch-id` command: prints `<patch-id> <commit>` per
//! revision. `tests/oracle/patch_id_oracle.sh` compares it with `git patch-id --stable`.
use std::path::Path;

use crate::error::{Error, Result};
use crate::{patch_id, repo};

pub fn patch_ids(repo_path: &Path, revs: &[String]) -> Result<()> {
    let repo = repo::open(repo_path)?;
    for rev in revs {
        let id = repo
            .rev_parse_single(rev.as_str())
            .map_err(|e| Error::usage(format!("{rev}: {e}")))?
            .detach();
        match patch_id::commit_patch_id(&repo, id)? {
            Some(p) => println!("{p} {id}"),
            None => println!("none {id}"),
        }
    }
    Ok(())
}
