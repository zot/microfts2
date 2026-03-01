# CLI
**Requirements:** R1, R34, R39, R41, R48, R49, R50, R57, R58, R59, R60, R61, R62, R70, R71, R72, R73, R74

Thin wrapper over DB library API. Parses subcommands and flags, delegates to DB, formats output.

## Knows
- db: DB instance

## Does
- init: create database (--charset, --case-insensitive, --content-db, --index-db)
- add: add files with a chunking strategy (--strategy)
- search: query and print `filepath:startline-endline` per result
- delete: remove a file
- reindex: reindex a file with a new strategy (--strategy)
- build-index: rebuild index (--cutoff)
- strategy add/remove/list: manage chunking strategies
- stale: list stale/missing files as `status\tpath`
- -r (global flag): refresh stale files before subcommand; alone = refresh + exit
- chunk-lines: output byte offsets at each newline
- chunk-lines-overlap: output byte offsets for overlapping line windows (--lines, --overlap)
- chunk-words-overlap: output byte offsets for overlapping word windows (--words, --overlap, --pattern)

## Collaborators
- DB: all operations delegate to DB (chunk-* commands are standalone, no DB needed)

## Sequences
- seq-init.md
- seq-add.md
- seq-search.md
- seq-build-index.md
- seq-stale.md
