# CLI

All commands require `-db <path>`. Optional shared flag: `-db-name` (subdatabase name, default "fts").

- `microfts init -db <path> [-case-insensitive] [-aliases <from=to,...>]`
  Create a new database.
- `microfts add -db <path> -strategy <name> <file>...`
  Add files using the named chunking strategy.
- `microfts search -db <path> [-regex] [-score coverage|density] [-verify] <query>...`
  Search for text. Builds index on demand if needed. Output: `filepath:range`
  With `-regex`, query is a Go regexp pattern; trigram query extracted from the regex AST.
  With `-score`, select scoring strategy (default: coverage).
  With `-verify`, post-filter results: for each candidate chunk, re-chunk the file using the stored chunking strategy to recover the chunk content (same text the trigrams were built from), tokenize the query into terms, and verify that every term appears as a case-insensitive substring in the chunk content. Chunks that fail are discarded. This eliminates false positives where trigrams match independently but the actual words are absent.
  Query tokenization for verify: split on spaces, but quoted strings (double quotes) are treated as a single term with quotes stripped. E.g. `"hello world" foo` produces terms `hello world` and `foo`.
- `microfts delete -db <path> <file>...`
  Remove files from the database.
- `microfts reindex -db <path> -strategy <name> <file>...`
  Re-chunk and reindex files with a different strategy.
- `microfts strategy add -db <path> -name <name> -cmd <command>`
  Register a chunking strategy.
- `microfts strategy remove -db <path> -name <name>`
  Remove a chunking strategy.
- `microfts strategy list -db <path>`
  List registered strategies.
- `microfts chunk-lines <file>`
  Output chunks for line-based chunking (`range\tcontent` per line).
- `microfts chunk-lines-overlap [-lines N] [-overlap M] <file>`
  Output chunks for overlapping line windows (`range\tcontent` per chunk).
- `microfts chunk-words-overlap [-words N] [-overlap M] [-pattern P] <file>`
  Output chunks for overlapping word windows (`range\tcontent` per chunk).
- `microfts chunk-markdown <file>`
  Output chunks for markdown paragraph-based chunking (`range\tcontent` per chunk).
- `microfts stale -db <path>`
  List all stale and missing files. Output: one line per file, `status\tpath` (tab-separated).
- `microfts score -db <path> [-score coverage|density] <query> <file>...`
  Score named files against a query. Output: one line per chunk, `filepath:range\tscore`.
- `microfts chunks -db <path> [-before N] [-after N] <file> <range>`
  Retrieve a target chunk and its neighbors. Looks up the file's chunk list from the F record, finds the target by range label match, returns the target plus up to N chunks before and after. Output: JSONL, one object per chunk with `path`, `range`, `content` fields. The target chunk is always included; neighbors are positional (chunk index ± N). Requires re-chunking the file to recover content. `-before` and `-after` default to 0.

- `-r` flag (global, before subcommand):
  Refresh all stale files before executing the subcommand. Uses each file's existing chunking strategy.
  - `microfts -r -db <path>` — refresh only, no subcommand
  - `microfts search -r -db <path> <query>` — refresh then search
  - When used without a subcommand, just refreshes and exits (printing refreshed files)
  - Missing files are reported but not deleted
