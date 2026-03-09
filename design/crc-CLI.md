# CLI
**Requirements:** R1, R34, R39, R41, R49, R50, R57, R58, R59, R60, R61, R62, R70, R71, R72, R73, R74, R89, R100, R108, R126, R132, R133, R175

Thin wrapper over DB library API. Parses subcommands and flags, delegates to DB, formats output.

## Knows
- db: DB instance

## Does
- init: create database (--case-insensitive, --content-db, --index-db)
- add: add files with a chunking strategy (--strategy)
- search: query and print `filepath:range` per result; --regex for regex pattern mode; --score coverage|density; --verify for post-filter verification
- delete: remove a file
- reindex: reindex a file with a new strategy (--strategy)
- strategy add/remove/list: manage chunking strategies
- stale: list stale/missing files as `status\tpath`
- score: score named files against a query, print `filepath:range\tscore` per chunk; --score coverage|density
- -r (global flag): refresh stale files before subcommand; alone = refresh + exit
- chunk-lines: output `range\tcontent` lines for line-based chunking
- chunk-lines-overlap: output `range\tcontent` for overlapping line windows (--lines, --overlap)
- chunk-words-overlap: output `range\tcontent` for overlapping word windows (--words, --overlap, --pattern)
- chunk-markdown: output `range\tcontent` for markdown paragraph-based chunking

## Collaborators
- DB: all operations delegate to DB (chunk-* commands are standalone, no DB needed)

## Sequences
- seq-init.md
- seq-add.md
- seq-search.md
- seq-score.md
- seq-stale.md
