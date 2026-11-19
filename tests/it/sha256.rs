//! SHA-256 of `src/sha256.rs`: the FIPS 180-4 vectors and the block boundaries (digests from `hashlib`).
use stw::sha256::hex;

#[test]
fn nist_vectors() {
    assert_eq!(
        hex(b""),
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
    assert_eq!(
        hex(b"abc"),
        "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );
    assert_eq!(
        hex(b"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"),
        "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"
    );
}

#[test]
fn one_million_a() {
    assert_eq!(
        hex(&vec![b'a'; 1_000_000]),
        "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"
    );
}

#[test]
fn padding_boundaries() {
    // 55 bytes: the length still fits the first block; 56: it does not; 64, 65: block edge; 119/120: second edge
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
            57,
            "f13b2d724659eb3bf47f2dd6af1accc87b81f09f59f2b75e5c0bed6589dfe8c6",
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
        (
            119,
            "31eba51c313a5c08226adf18d4a359cfdfd8d2e816b13f4af952f7ea6584dcfb",
        ),
        (
            120,
            "2f3d335432c70b580af0e8e1b3674a7c020d683aa5f73aaaedfdc55af904c21c",
        ),
        (
            128,
            "6836cf13bac400e9105071cd6af47084dfacad4e5e302c94bfed24e013afb73e",
        ),
    ] {
        assert_eq!(hex(&vec![b'a'; n]), want, "{n} bytes");
    }
}

#[test]
fn all_byte_values_and_utf8() {
    let all: Vec<u8> = (0..=255u8).cycle().take(1024).collect();
    assert_eq!(
        hex(&all),
        "785b0751fc2c53dc14a4ce3d800e69ef9ce1009eb327ccf458afe09c242c26c9"
    );
    assert_eq!(
        hex("äöü 中".as_bytes()),
        "c5a1ca2e6560fa286095356c59a751a8d45149025ba85188aa1672f3aaf714a4"
    );
}
