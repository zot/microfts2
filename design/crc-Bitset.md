# Bitset
**Requirements:** R6

Fixed-size bitset for 16,777,216 trigrams (2^24). Stored as 2,097,152 bytes (2MB). Exported type for use by Ark and other consumers. Not used internally by microfts2 (C records use sparse LMDB records; trigram selection is dynamic via TrigramFilter).

## Knows
- data: [2097152]byte

## Does
- Set(trigram uint32): set bit at position trigram
- Test(trigram uint32): return whether bit is set
- ForEach(fn func(uint32)): call fn for each set bit
- Bytes(): return data slice for storage
- FromBytes(b []byte): load from stored bytes
- Count(): return number of set bits

## Collaborators
- none (leaf type, not used by DB)

## Sequences
- (none — not used in any DB sequence)
