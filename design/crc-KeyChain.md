# KeyChain
**Requirements:** R25, R26

Encodes and decodes filenames for index F records. Filenames within 509 bytes use a single key. Longer filenames are split across chained keys using the name-part byte. (bbolt allows up to 32768-byte keys, so the chaining is no longer forced by the engine, but the 511-byte threshold is retained as a legacy convention and remains correct.)

Key format: `'F' [namepart: 1] [filename-segment]`
- namepart 0..254: non-final segment (value: empty)
- namepart 255: final segment (value: fileid as 8-byte big-endian uint64)

Max segment length: 511 - 2 = 509 bytes (legacy 511-byte key threshold minus F prefix and namepart byte; retained even though bbolt's key limit is 32768 bytes).

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
