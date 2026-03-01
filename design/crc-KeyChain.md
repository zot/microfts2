# KeyChain
**Requirements:** R25, R26

Encodes and decodes filenames for LMDB F records. Filenames within 509 bytes use a single key. Longer filenames are split across chained keys using the name-part byte.

Key format: `'F' [namepart: 1] [filename-segment]`
- namepart 0..254: non-final segment (value: empty)
- namepart 255: final segment (value: fileid as 8-byte big-endian uint64)

Max segment length: 511 - 2 = 509 bytes (LMDB key limit minus F prefix and namepart byte).

## Knows
- (stateless — pure functions)

## Does
- Encode(filename): return list of key/value pairs for F records
- FinalKey(filename): return the final key (namepart=255) for direct lookup
- DecodeFilename(keys): reconstruct full filename from chained F record keys

## Collaborators
- none (leaf type)

## Sequences
- seq-add.md
