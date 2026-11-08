# Review and carry-over first lines

Examples for the machine-readable first line of a review comment and of a carry-over comment.
`test.sh` checks every line starting with `ok: ` against the format regex (must match) and every line starting with `bad: `
(must not match). The text after the prefix is the line under test.

Format:

    Review: <CLEAR|FINDINGS> head=<40-hex SHA> patch-id=<40-hex> pr=<n>
    Carried-over: from=<40-hex SHA> to=<40-hex SHA> patch-id=<40-hex> pr=<n>

Valid review lines:

ok: Review: CLEAR head=0123456789abcdef0123456789abcdef01234567 patch-id=89abcdef0123456789abcdef0123456789abcdef pr=15
ok: Review: FINDINGS head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=1

Valid carry-over lines:

ok: Carried-over: from=0123456789abcdef0123456789abcdef01234567 to=fedcba9876543210fedcba9876543210fedcba98 patch-id=89abcdef0123456789abcdef0123456789abcdef pr=15

Invalid review lines:

bad: Review: CLEAR head=0123456 patch-id=89abcdef0123456789abcdef0123456789abcdef pr=15
bad: Review: CLEAR patch-id=89abcdef0123456789abcdef0123456789abcdef head=0123456789abcdef0123456789abcdef01234567 pr=15
bad: Review: CLEAR head=0123456789abcdef0123456789abcdef01234567 patch-id=89abcdef0123456789abcdef0123456789abcdef
bad: Verdict: CLEAR head=0123456789abcdef0123456789abcdef01234567 patch-id=89abcdef0123456789abcdef0123456789abcdef pr=15
bad: Review: MAYBE head=0123456789abcdef0123456789abcdef01234567 patch-id=89abcdef0123456789abcdef0123456789abcdef pr=15

Invalid carry-over lines:

bad: Carried-over: from=0123456 to=fedcba9876543210fedcba9876543210fedcba98 patch-id=89abcdef0123456789abcdef0123456789abcdef pr=15
bad: Carried-over: to=fedcba9876543210fedcba9876543210fedcba98 from=0123456789abcdef0123456789abcdef01234567 patch-id=89abcdef0123456789abcdef0123456789abcdef pr=15
bad: Carried-over: from=0123456789abcdef0123456789abcdef01234567 to=fedcba9876543210fedcba9876543210fedcba98 patch-id=89abcdef0123456789abcdef0123456789abcdef

More invalid lines (39 or 41 digits, uppercase hex, other verdicts, spacing, markdown, empty pr):

bad: Review: CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: CLEAR head=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: CLEAR head=gggggggggggggggggggggggggggggggggggggggg patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: OK head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: clear head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review:  CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: CLEAR  head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad:  Review: CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: `Review: CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15`
bad: > Review: CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Review: CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=
bad: Carried-over: from=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa to=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb patch-id=cccccccccccccccccccccccccccccccccccccccc pr=15
bad:  Carried-over: from=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa to=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb patch-id=cccccccccccccccccccccccccccccccccccccccc pr=15
bad: Review:CLEAR head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa patch-id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=15
bad: Carried.over: from=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa to=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb patch-id=cccccccccccccccccccccccccccccccccccccccc pr=15
bad: Carried-over: from=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bis=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb patch-id=cccccccccccccccccccccccccccccccccccccccc pr=15

Tab and CRLF cases cannot be shown in a markdown line; `test.sh` builds them inline.
