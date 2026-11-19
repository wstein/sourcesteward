//! The watch core (port package W1): `src/watch/`. Pure functions and files of the state directory; the replay of the
//! scenario corpus (`tests/fixtures/watch/`) is at the end of this file.
use stw::watch::statedir::{sha256, sha256_hex, workspace_id, IdSource};

#[test]
fn sha256_matches_the_nist_vectors() {
    // FIPS 180-4 examples and the NIST CAVS short and long message boundaries (block size 64, padding at 55/56/64).
    assert_eq!(
        sha256_hex(b""),
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
    assert_eq!(
        sha256_hex(b"abc"),
        "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );
    assert_eq!(
        sha256_hex(b"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"),
        "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"
    );
    assert_eq!(
        sha256_hex(
            b"abcdefghbcdefghicdefghijdefghijkefghijklfghijklmghijklmnhijklmnoijklmnopjklmnopqklmnopqrlmnopqrsmnopqrstnopqrstu"
        ),
        "cf5b16a778af8380036ce59e7b0492370b249b11e8f07a51afac45037afee9d1"
    );
    assert_eq!(
        sha256_hex(&vec![b'a'; 1_000_000]),
        "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"
    );
    assert_eq!(sha256(b"abc")[0], 0xba);
}

#[test]
fn sha256_padding_boundaries_agree_with_sha256sum() {
    // Lengths around the padding boundaries; the digests were made with GNU `sha256sum` over `head -c N /dev/zero | tr '\0' a`.
    for (n, want) in [
        (
            55,
            "9f4390f8d30c2dd92ec9f095b65e2b9ae9b0a925a5258e241c9f1e910f734318",
        ),
        (
            56,
            "b35439a4ac6f0948b6d6f9e3c6af0f5f590ce20f1bde7090ef7970686ec6738a",
        ),
        (
            63,
            "7d3e74a05d7db15bce4ad9ec0658ea98e3f06eeecf16b4c6fff2da457ddc2f34",
        ),
        (
            64,
            "ffe054fe7ae0cb6dc65c3af9b61d5209f439851db43d0ba5997337df154668eb",
        ),
        (
            65,
            "635361c48bb9eab14198e76ea8ab7f1a41685d6ad62aa9146d301d4f17eb0ae0",
        ),
    ] {
        assert_eq!(sha256_hex(&vec![b'a'; n]), want, "length {n}");
    }
}

#[test]
fn the_workspace_id_is_sixteen_hex_digits_of_the_prefixed_hash() {
    // printf 'stw-watch-rs1\ngithub.com\nrepos:a/b' | sha256sum
    let a_b = ["a/b".to_string()];
    assert_eq!(
        workspace_id("github.com", IdSource::Repos(&a_b)),
        "0465033cc48d07a5"
    );
    // ... 'repos:a/b\nc/d'
    let two = ["c/d".to_string(), "a/b".to_string()];
    assert_eq!(
        workspace_id("github.com", IdSource::Repos(&two)),
        "431f3da077950d4c"
    );
    // ... 'workspace:/w/root'
    assert_eq!(
        workspace_id("github.com", IdSource::Workspace("/w/root")),
        "8633f7f15d37c015"
    );
    // ... host ghe.example
    assert_eq!(
        workspace_id("ghe.example", IdSource::Repos(&a_b)),
        "c7ecea5836adf0b7"
    );
    // no repositories: the text ends after `repos:`
    assert_eq!(
        workspace_id("github.com", IdSource::Repos(&[])),
        "d6b4026bb7cb9651"
    );
}

#[test]
fn the_repository_list_is_lower_cased_deduplicated_and_sorted_before_hashing() {
    let base = ["a/b".to_string(), "c/d".to_string()];
    let id = workspace_id("github.com", IdSource::Repos(&base));
    let shuffled = [
        "C/D".to_string(),
        "a/b".to_string(),
        "A/B".to_string(),
        "c/d".to_string(),
    ];
    assert_eq!(workspace_id("github.com", IdSource::Repos(&shuffled)), id);
    assert_eq!(id.len(), 16);
    assert!(id
        .bytes()
        .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)));
    assert_ne!(workspace_id("github.com", IdSource::Workspace("/a")), id);
    assert_ne!(
        workspace_id("github.com", IdSource::Workspace("/a")),
        workspace_id("github.com", IdSource::Workspace("/b"))
    );
}
