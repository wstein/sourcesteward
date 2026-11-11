#!/bin/bash
# Proof that stw starts neither git nor gh: fake `git` and `gh` first on PATH log every call; then the
# binary runs all commands with PATH=/nonexistent as well. Usage: shim_proof.sh STW_BINARY ROOT SCRATCH
# `tidy apply` really deletes the branches the plan marks `delete` in ROOT: use a scratch workspace.
set -u
STW=$1 ROOT=$2 SCRATCH=$3
D=$(mktemp -d "$SCRATCH/shim.XXXXXX"); LOG=$D/calls.log; : > "$LOG"
for t in git gh ssh git-upload-pack; do printf '#!/bin/sh\necho "%s $*" >> "%s"\nexit 99\n' "$t" "$LOG" > "$D/$t"; chmod +x "$D/$t"; done
run() { env -i HOME="$HOME" PATH="$1" "$STW" "${@:2}" > /dev/null 2>&1; echo "  exit $? : stw ${*:2}"; }
echo "fake git/gh first on PATH:"
for a in "status --root $ROOT" "tidy plan --root $ROOT" "sync --plan --root $ROOT" "status --root $ROOT --json" "tidy apply --root $ROOT --manifest-dir $D/m" "tidy restore --manifest-dir $D/none"; do run "$D:/usr/bin:/bin" $a; done
echo "PATH=/nonexistent:"
for a in "status --root $ROOT" "tidy plan --root $ROOT" "sync --plan --root $ROOT" "tidy apply --root $ROOT --manifest-dir $D/m"; do run /nonexistent $a; done
echo "calls to fake git/gh/ssh: $(wc -l < "$LOG" | tr -d ' ')"
[ ! -s "$LOG" ]
