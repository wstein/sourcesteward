# Validation quickstart
Run `go test ./internal/contracts -v` after feature005 is integrated, then `make check`. Tests compile the schemas with network-disabled loading, validate real CLI output in temporary repositories, reject independent invalid fixtures and ensure the runtime graph excludes the validator. No Git metadata from the user's checkout is mutated.
Build the current site artifact and compare schemas/v1 files with generated public/schemas/v1 files. Public URLs become live only after approved merge and successful exact-revision Pages publication.
