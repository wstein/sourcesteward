//! HTTPS for every `ureq` client in stw.
//!
//! The platform trust store is used, which on Linux is the system CA bundle. Certificate
//! verification stays on. The Mozilla roots bundled with ureq are not used: a proxy certificate
//! signed by a CA in the system bundle is then accepted, and an untrusted certificate is still refused.
pub(crate) fn config() -> ureq::tls::TlsConfig {
    ureq::tls::TlsConfig::builder()
        .root_certs(ureq::tls::RootCerts::PlatformVerifier)
        .build()
}

#[cfg(test)]
mod tests {
    #[test]
    fn the_client_trusts_the_platform_store_and_still_verifies() {
        let tls = super::config();
        assert_eq!(
            format!("{:?}", tls.root_certs()),
            "PlatformVerifier",
            "the bundled Mozilla roots are still selected"
        );
        assert!(
            !tls.disable_verification(),
            "certificate verification must stay on"
        );
    }
}
