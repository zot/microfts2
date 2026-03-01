# Test Design: Bitset
**Source:** crc-Bitset.md

## Test: set and test
**Purpose:** basic bit operations
**Input:** Set(0), Set(100), Set(262143)
**Expected:** Test(0)=true, Test(100)=true, Test(262143)=true, Test(1)=false
**Refs:** crc-Bitset.md

## Test: bytes roundtrip
**Purpose:** serialize and deserialize
**Input:** Set several trigrams, Bytes(), FromBytes()
**Expected:** all originally set bits still set, no others
**Refs:** crc-Bitset.md

## Test: forEach
**Purpose:** iterate set bits
**Input:** Set(5), Set(1000), Set(200000)
**Expected:** ForEach yields exactly [5, 1000, 200000] in order
**Refs:** crc-Bitset.md

## Test: count
**Purpose:** count set bits
**Input:** Set 50 distinct trigrams
**Expected:** Count() returns 50
**Refs:** crc-Bitset.md

## Test: empty bitset
**Purpose:** no bits set
**Input:** new Bitset, no Set calls
**Expected:** Count()=0, ForEach yields nothing, Bytes() is 32KB of zeros
**Refs:** crc-Bitset.md
