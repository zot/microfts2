# DB
**Requirements:** R1, R2, R14, R15, R16, R17, R18, R19, R20, R21, R27, R28, R29, R30, R33, R35, R36, R37, R38, R39, R40, R41, R42, R43, R44, R22, R23, R24, R32, R51, R52, R53, R54, R55, R56

Main database handle. Manages LMDB environment with two named subdatabases (content and index). Provides the public library API.

## Knows
- env: LMDB environment handle
- contentDBI: content subdatabase handle
- indexDBI: index subdatabase handle (nil if not built)
- settings: loaded from I record (character set, chunking strategies, case-insensitive, active trigrams, cutoff)
- charSet: CharSet instance configured from settings
- contentName: subdatabase name (default "ftscontent")
- indexName: subdatabase name (default "ftsindex")

## Does
- Create(path, opts): create new database, write I record with settings, write zeroed C record
- Open(path, opts): open existing database, load settings from I record
- Close(): close LMDB environment
- AddFile(path, strategy): chunk file, compute bitsets, store F/T/N records, update C counts
- RemoveFile(path): remove F/T/N records, update C counts, drop index
- Search(query): build index if needed, extract trigrams, intersect posting lists, return results
- BuildIndex(cutoff): compute active trigrams from C counts at cutoff, rebuild index from T records
- Reindex(path, strategy): remove old records, re-add with new strategy
- AddStrategy(name, cmd): add chunking strategy to I record
- RemoveStrategy(name): remove chunking strategy from I record

## Collaborators
- CharSet: trigram extraction from text
- Bitset: per-chunk trigram bitset
- Chunker: execute external chunking commands
- KeyChain: encode/decode long filenames in F records

## Sequences
- seq-init.md
- seq-add.md
- seq-search.md
- seq-build-index.md
