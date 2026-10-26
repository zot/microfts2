# KeyChain
**Requirements:** R25, R26, R740

Encodes and decodes filenames for index N records. Filenames within 509 bytes use a single key. Longer filenames are split across chained keys using the name-part byte. (bbolt allows up to 32768-byte keys, so the chaining is no longer forced by the engine, but the 511-byte threshold is retained as a legacy convention and remains correct.)

Key format: `'N' [namepart: 1] [filename-segment]`
- namepart 0..254: non-final segment (value: empty)
- namepart 255: final segment (value: length-prefixed full name + fileid varint, written by DB)

Max segment length: 511 - 2 = 509 bytes (legacy 511-byte key threshold minus N prefix and namepart byte; retained even though bbolt's key limit is 32768 bytes).

## Knows
- (stateless — pure functions). They encode exactly the string given; case folding is DB's, applied before the call (R740)

## Does
- EncodeFilename(filename): return list of key/value pairs for N records
- FinalKey(filename): return the final key (namepart=255) for direct lookup
- DecodeFilename(keys): reconstruct full filename from chained N record keys

## Collaborators
- none (leaf type)

## Sequences
- seq-add.md
