//! Error type shared by all modules. Messages say what failed and, where there is one, what to do next.
use std::fmt;

/// What kind of failure this is; the CLI maps it to an exit code and a hint.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    /// Reading or writing a repository (gix, files).
    Repository,
    /// GitHub REST answered with an error status or could not be reached.
    GitHub,
    /// Wrong arguments or workspace (no root, unknown repository name).
    Usage,
}

#[derive(Debug)]
pub struct Error {
    kind: Kind,
    message: String,
}

pub type Result<T> = std::result::Result<T, Error>;

impl Error {
    pub fn new(kind: Kind, message: impl Into<String>) -> Error {
        Error {
            kind,
            message: message.into(),
        }
    }
    pub fn repository(message: impl Into<String>) -> Error {
        Error::new(Kind::Repository, message)
    }
    pub fn github(message: impl Into<String>) -> Error {
        Error::new(Kind::GitHub, message)
    }
    pub fn usage(message: impl Into<String>) -> Error {
        Error::new(Kind::Usage, message)
    }
    pub fn kind(&self) -> Kind {
        self.kind
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}

/// gix and I/O errors are repository errors. `Error` itself does not implement `std::error::Error`,
/// which keeps this blanket impl coherent.
impl<E: std::error::Error + Send + Sync + 'static> From<E> for Error {
    fn from(e: E) -> Error {
        Error::repository(e.to_string())
    }
}

/// Process exit codes of `stw` (documented in `--help`).
pub mod exit {
    /// Everything that was asked for worked.
    pub const OK: u8 = 0;
    /// The command ran, but at least one repository could not be read or `--apply` failed on one.
    pub const FAILED: u8 = 1;
    /// Usage error or unusable workspace; nothing was done.
    pub const USAGE: u8 = 2;
}
