# Bitset
**Requirements:** R7, R8

Fixed-size bitset for 262,144 trigrams (2^18). Stored as 32,768 bytes (32KB).

## Knows
- data: [32768]byte

## Does
- Set(trigram uint32): set bit at position trigram
- Test(trigram uint32): return whether bit is set
- ForEach(fn func(uint32)): call fn for each set bit
- Bytes(): return data slice for LMDB storage
- FromBytes(b []byte): load from stored bytes
- Count(): return number of set bits

## Collaborators
- none (leaf type)

## Sequences
- seq-add.md
- seq-search.md
- seq-build-index.md
