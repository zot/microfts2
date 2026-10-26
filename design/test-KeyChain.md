# Test Design: KeyChain
**Source:** crc-KeyChain.md

## Test: short filename
**Purpose:** a name that fits one key encodes as a single final N key
**Input:** `EncodeFilename("/tmp/test.txt")`
**Expected:** one pair: prefix `N`, part byte `nFinalPart`, payload is the name
**Refs:** crc-KeyChain.md, R26
**Code:** keychain_test.go

## Test: long filename
**Purpose:** a name longer than one part chains across several N keys
**Input:** a name longer than `maxPartLen`
**Expected:** at least 2 pairs; parts numbered 0, 1, …; the last is `nFinalPart`
**Refs:** crc-KeyChain.md, R25, R26
**Code:** keychain_test.go

## Test: decode roundtrip
**Purpose:** encoding then decoding returns the original name
**Input:** names of several lengths, short and chained
**Expected:** each decodes to the original bytes
**Refs:** crc-KeyChain.md, R25, R26
**Code:** keychain_test.go

## Test: FinalKey, short name
**Purpose:** FinalKey is the lookup key without encoding the whole chain
**Input:** a short name
**Expected:** FinalKey equals the single EncodeFilename key
**Refs:** crc-KeyChain.md, R26
**Code:** keychain_test.go

## Test: FinalKey, long name
**Purpose:** FinalKey for a chained name is the chain's final key
**Input:** a name longer than `maxPartLen`
**Expected:** FinalKey equals the last EncodeFilename key
**Refs:** crc-KeyChain.md, R25, R26
**Code:** keychain_test.go

## Test: part-length boundary
**Purpose:** the chain splits exactly past `maxPartLen`
**Input:** names of exactly `maxPartLen` and `maxPartLen+1` bytes
**Expected:** 1 pair, then 2 pairs
**Refs:** crc-KeyChain.md, R25
**Code:** keychain_test.go
