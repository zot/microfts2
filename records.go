package microfts2

// CRC: crc-DB.md | R220, R221, R222, R244, R245, R246, R247, R248, R249, R250, R251, R252, R264, R265, R266, R267, R295

import (
	"encoding/binary"
	"fmt"

	"github.com/bmatsuo/lmdb-go/lmdb"
)

// TxnHolder is anything that carries an LMDB transaction.
// CRecord implements it; txnWrap wraps raw transactions from View/Update blocks.
type TxnHolder interface {
	Txn() *lmdb.Txn
}

// txnWrap wraps a raw *lmdb.Txn into a TxnHolder.
type txnWrap struct{ txn *lmdb.Txn }

func (w txnWrap) Txn() *lmdb.Txn { return w.txn }

// TrigramEntry pairs a trigram code with its per-chunk occurrence count.
type TrigramEntry struct {
	Trigram uint32
	Count   int
}

// TokenEntry pairs a token string with its occurrence count.
type TokenEntry struct {
	Token string
	Count int
}

// FileChunkEntry pairs a chunkid with its location label (opaque range string from chunker).
type FileChunkEntry struct {
	ChunkID  uint64
	Location string
}

// CRecord is the per-chunk record. Self-describing: everything needed
// for search, scoring, filtering, and removal.
// Carries unexported db/txn — the chunk is tied to the transaction that read it.
type CRecord struct {
	ChunkID  uint64
	Hash     [32]byte
	Trigrams []TrigramEntry
	Tokens   []TokenEntry
	Attrs    []Pair
	FileIDs  []uint64
	db       *DB
	txn      *lmdb.Txn
}

// attach sets the db and txn context for a CRecord.
func (c *CRecord) attach(db *DB, txn *lmdb.Txn) {
	c.db = db
	c.txn = txn
}

// Txn returns the transaction this record was read from. Implements TxnHolder.
func (c *CRecord) Txn() *lmdb.Txn { return c.txn }

// DB returns the database this record belongs to.
func (c *CRecord) DB() *DB { return c.db }

// FileRecord navigates to an F record within the same transaction.
func (c *CRecord) FileRecord(fileid uint64) (FRecord, error) {
	return c.db.readFRecord(c, fileid)
}

// FRecord is the per-file record. Metadata, ordered chunks, file-level token bag.
type FRecord struct {
	FileID      uint64
	ModTime     int64
	ContentHash [32]byte
	FileLength  int64
	Strategy    string
	Names       []string
	Chunks      []FileChunkEntry
	Tokens      []TokenEntry
}

// TRecord is the trigram inverted index entry.
type TRecord struct {
	Trigram  uint32
	ChunkIDs []uint64
}

// WRecord is the token inverted index entry.
type WRecord struct {
	TokenHash uint32
	ChunkIDs  []uint64
}

// HRecord maps content hash to chunkid.
type HRecord struct {
	Hash    [32]byte
	ChunkID uint64
}

// --- Encoding helpers ---

func putUvarint(buf []byte, v uint64) int { return binary.PutUvarint(buf, v) }
func readUvarint(buf []byte) (uint64, int) {
	v, n := binary.Uvarint(buf)
	return v, n
}

func putString(buf []byte, s string) int {
	n := binary.PutUvarint(buf, uint64(len(s)))
	n += copy(buf[n:], s)
	return n
}

func readString(buf []byte) (string, int) {
	l, n := binary.Uvarint(buf)
	if n <= 0 || int(l) > len(buf)-n {
		return "", 0
	}
	s := string(buf[n : n+int(l)])
	return s, n + int(l)
}

func putBytes(buf, b []byte) int {
	n := binary.PutUvarint(buf, uint64(len(b)))
	n += copy(buf[n:], b)
	return n
}

func readBytes(buf []byte) ([]byte, int) {
	l, n := binary.Uvarint(buf)
	if n <= 0 || int(l) > len(buf)-n {
		return nil, 0
	}
	b := make([]byte, l)
	copy(b, buf[n:n+int(l)])
	return b, n + int(l)
}

// --- CRecord marshal/unmarshal ---

// MarshalValue encodes the CRecord value (everything except the key prefix and chunkid).
// v2 format: hash + trigrams + tokens + attrs + fileids
func (c *CRecord) MarshalValue() []byte {
	// Estimate size: hash(32) + trigrams + tokens + attrs + fileids
	size := 32 // hash
	size += binary.MaxVarintLen64 // n-trigrams
	size += len(c.Trigrams) * (3 + binary.MaxVarintLen64)
	size += binary.MaxVarintLen64 // n-tokens
	for _, t := range c.Tokens {
		size += binary.MaxVarintLen64 + binary.MaxVarintLen64 + len(t.Token)
	}
	size += binary.MaxVarintLen64 // n-attrs
	for _, p := range c.Attrs {
		size += binary.MaxVarintLen64 + len(p.Key) + binary.MaxVarintLen64 + len(p.Value)
	}
	size += binary.MaxVarintLen64 // n-fileids
	size += len(c.FileIDs) * binary.MaxVarintLen64

	buf := make([]byte, size)
	off := 0

	// Hash
	copy(buf[off:], c.Hash[:])
	off += 32

	// Trigrams
	off += putUvarint(buf[off:], uint64(len(c.Trigrams)))
	for _, te := range c.Trigrams {
		buf[off] = byte(te.Trigram >> 16)
		buf[off+1] = byte(te.Trigram >> 8)
		buf[off+2] = byte(te.Trigram)
		off += 3
		off += putUvarint(buf[off:], uint64(te.Count))
	}

	// Tokens
	off += putUvarint(buf[off:], uint64(len(c.Tokens)))
	for _, te := range c.Tokens {
		off += putUvarint(buf[off:], uint64(te.Count))
		off += putString(buf[off:], te.Token)
	}

	// Attrs
	off += putUvarint(buf[off:], uint64(len(c.Attrs)))
	for _, p := range c.Attrs {
		off += putBytes(buf[off:], p.Key)
		off += putBytes(buf[off:], p.Value)
	}

	// FileIDs
	off += putUvarint(buf[off:], uint64(len(c.FileIDs)))
	for _, fid := range c.FileIDs {
		off += putUvarint(buf[off:], fid)
	}

	return buf[:off]
}

// UnmarshalCValue decodes a CRecord value. ChunkID must be set separately (from key).
// v2 format: hash + trigrams + tokens + attrs + fileids
func UnmarshalCValue(data []byte) (CRecord, error) {
	var c CRecord
	if len(data) < 32 {
		return c, fmt.Errorf("CRecord too short: %d bytes", len(data))
	}
	off := 0

	// Hash
	copy(c.Hash[:], data[off:off+32])
	off += 32

	// Trigrams
	nTri, n := readUvarint(data[off:])
	if n <= 0 {
		return c, fmt.Errorf("CRecord: bad n-trigrams")
	}
	off += n
	c.Trigrams = make([]TrigramEntry, nTri)
	for i := range c.Trigrams {
		if off+3 > len(data) {
			return c, fmt.Errorf("CRecord: trigram truncated")
		}
		c.Trigrams[i].Trigram = uint32(data[off])<<16 | uint32(data[off+1])<<8 | uint32(data[off+2])
		off += 3
		cnt, n := readUvarint(data[off:])
		if n <= 0 {
			return c, fmt.Errorf("CRecord: bad trigram count")
		}
		c.Trigrams[i].Count = int(cnt)
		off += n
	}

	// Tokens
	nTok, n := readUvarint(data[off:])
	if n <= 0 {
		return c, fmt.Errorf("CRecord: bad n-tokens")
	}
	off += n
	c.Tokens = make([]TokenEntry, nTok)
	for i := range c.Tokens {
		cnt, n := readUvarint(data[off:])
		if n <= 0 {
			return c, fmt.Errorf("CRecord: bad token count")
		}
		c.Tokens[i].Count = int(cnt)
		off += n
		s, n := readString(data[off:])
		if n == 0 && nTok > 0 {
			return c, fmt.Errorf("CRecord: bad token string")
		}
		c.Tokens[i].Token = s
		off += n
	}

	// Attrs
	nAttr, n := readUvarint(data[off:])
	if n <= 0 {
		return c, fmt.Errorf("CRecord: bad n-attrs")
	}
	off += n
	if nAttr > 0 {
		c.Attrs = make([]Pair, nAttr)
	}
	for i := 0; i < int(nAttr); i++ {
		k, n := readBytes(data[off:])
		if n == 0 {
			return c, fmt.Errorf("CRecord: bad attr key")
		}
		off += n
		v, n := readBytes(data[off:])
		if n == 0 {
			return c, fmt.Errorf("CRecord: bad attr value")
		}
		off += n
		c.Attrs[i] = Pair{Key: k, Value: v}
	}

	// FileIDs
	nFIDs, n := readUvarint(data[off:])
	if n <= 0 {
		return c, fmt.Errorf("CRecord: bad n-fileids")
	}
	off += n
	c.FileIDs = make([]uint64, nFIDs)
	for i := range c.FileIDs {
		fid, n := readUvarint(data[off:])
		if n <= 0 {
			return c, fmt.Errorf("CRecord: bad fileid")
		}
		c.FileIDs[i] = fid
		off += n
	}

	return c, nil
}

// --- FRecord marshal/unmarshal ---

// MarshalValue encodes the FRecord value (everything except the key prefix and fileid).
func (f *FRecord) MarshalValue() []byte {
	size := 8 + 32 // modTime + contentHash
	size += binary.MaxVarintLen64 // fileLength
	size += binary.MaxVarintLen64 + len(f.Strategy) // strategy
	size += binary.MaxVarintLen64 // filecount
	for _, name := range f.Names {
		size += binary.MaxVarintLen64 + len(name)
	}
	size += binary.MaxVarintLen64 // chunkcount
	for _, ch := range f.Chunks {
		size += binary.MaxVarintLen64 + binary.MaxVarintLen64 + len(ch.Location)
	}
	size += binary.MaxVarintLen64 // tokencount
	for _, t := range f.Tokens {
		size += binary.MaxVarintLen64 + len(t.Token) + binary.MaxVarintLen64
	}

	buf := make([]byte, size)
	off := 0

	// ModTime (fixed 8 bytes, big-endian)
	binary.BigEndian.PutUint64(buf[off:], uint64(f.ModTime))
	off += 8

	// ContentHash (fixed 32 bytes)
	copy(buf[off:], f.ContentHash[:])
	off += 32

	// FileLength
	off += putUvarint(buf[off:], uint64(f.FileLength))

	// Strategy
	off += putString(buf[off:], f.Strategy)

	// Names
	off += putUvarint(buf[off:], uint64(len(f.Names)))
	for _, name := range f.Names {
		off += putString(buf[off:], name)
	}

	// Chunks
	off += putUvarint(buf[off:], uint64(len(f.Chunks)))
	for _, ch := range f.Chunks {
		off += putUvarint(buf[off:], ch.ChunkID)
		off += putString(buf[off:], ch.Location)
	}

	// Tokens
	off += putUvarint(buf[off:], uint64(len(f.Tokens)))
	for _, t := range f.Tokens {
		off += putString(buf[off:], t.Token)
		off += putUvarint(buf[off:], uint64(t.Count))
	}

	return buf[:off]
}

// unmarshalFHeader decodes the header fields of an F record value:
// ModTime, ContentHash, FileLength, Strategy, and Names.
// Returns the record, the byte offset after Names, and any error.
func unmarshalFHeader(data []byte) (FRecord, int, error) {
	var f FRecord
	if len(data) < 40 {
		return f, 0, fmt.Errorf("FRecord too short: %d bytes", len(data))
	}
	off := 0

	f.ModTime = int64(binary.BigEndian.Uint64(data[off:]))
	off += 8

	copy(f.ContentHash[:], data[off:off+32])
	off += 32

	fl, n := readUvarint(data[off:])
	if n <= 0 {
		return f, 0, fmt.Errorf("FRecord: bad fileLength")
	}
	f.FileLength = int64(fl)
	off += n

	s, n := readString(data[off:])
	if n == 0 {
		return f, 0, fmt.Errorf("FRecord: bad strategy")
	}
	f.Strategy = s
	off += n

	nNames, n := readUvarint(data[off:])
	if n <= 0 {
		return f, 0, fmt.Errorf("FRecord: bad namecount")
	}
	off += n
	f.Names = make([]string, nNames)
	for i := range f.Names {
		s, n := readString(data[off:])
		if n == 0 {
			return f, 0, fmt.Errorf("FRecord: bad name")
		}
		f.Names[i] = s
		off += n
	}

	return f, off, nil
}

// UnmarshalFValue decodes an FRecord value. FileID must be set separately (from key).
func UnmarshalFValue(data []byte) (FRecord, error) {
	f, off, err := unmarshalFHeader(data)
	if err != nil {
		return f, err
	}

	// Chunks
	nChunks, n := readUvarint(data[off:])
	if n <= 0 {
		return f, fmt.Errorf("FRecord: bad chunkcount")
	}
	off += n
	f.Chunks = make([]FileChunkEntry, nChunks)
	for i := range f.Chunks {
		cid, n := readUvarint(data[off:])
		if n <= 0 {
			return f, fmt.Errorf("FRecord: bad chunkid")
		}
		f.Chunks[i].ChunkID = cid
		off += n
		loc, n := readString(data[off:])
		if n == 0 {
			return f, fmt.Errorf("FRecord: bad location")
		}
		f.Chunks[i].Location = loc
		off += n
	}

	// Tokens
	nTokens, n := readUvarint(data[off:])
	if n <= 0 {
		return f, fmt.Errorf("FRecord: bad tokencount")
	}
	off += n
	f.Tokens = make([]TokenEntry, nTokens)
	for i := range f.Tokens {
		tok, n := readString(data[off:])
		if n == 0 {
			return f, fmt.Errorf("FRecord: bad token")
		}
		f.Tokens[i].Token = tok
		off += n
		cnt, n := readUvarint(data[off:])
		if n <= 0 {
			return f, fmt.Errorf("FRecord: bad token count")
		}
		f.Tokens[i].Count = int(cnt)
		off += n
	}

	return f, nil
}

// R451, R452: UnmarshalFHeader decodes only the header fields of an F record value:
// ModTime, ContentHash, FileLength, Strategy, and Names. Skips Chunks and Tokens.
func UnmarshalFHeader(data []byte) (FRecord, error) {
	f, _, err := unmarshalFHeader(data)
	return f, err
}

// --- Shared chunkid list encoding ---

// marshalChunkIDs encodes a packed list of varint chunkids.
func marshalChunkIDs(ids []uint64) []byte {
	buf := make([]byte, len(ids)*binary.MaxVarintLen64)
	off := 0
	for _, cid := range ids {
		off += putUvarint(buf[off:], cid)
	}
	return buf[:off]
}

// --- TRecord marshal/unmarshal ---

// MarshalValue encodes the TRecord value (packed chunkid list).
func (t *TRecord) MarshalValue() []byte {
	return marshalChunkIDs(t.ChunkIDs)
}

// countTValue counts the number of chunkids in a T record value without allocating.
func countTValue(data []byte) int {
	n := 0
	off := 0
	for off < len(data) {
		_, size := readUvarint(data[off:])
		if size <= 0 {
			break
		}
		off += size
		n++
	}
	return n
}

// UnmarshalTValue decodes a TRecord value. Trigram must be set separately (from key).
func UnmarshalTValue(data []byte) ([]uint64, error) {
	var ids []uint64
	off := 0
	for off < len(data) {
		v, n := readUvarint(data[off:])
		if n <= 0 {
			return nil, fmt.Errorf("TRecord: bad chunkid at offset %d", off)
		}
		ids = append(ids, v)
		off += n
	}
	return ids, nil
}

// --- WRecord marshal/unmarshal ---

// MarshalValue encodes the WRecord value (packed chunkid list, same as TRecord).
func (w *WRecord) MarshalValue() []byte {
	return marshalChunkIDs(w.ChunkIDs)
}

// UnmarshalWValue decodes a WRecord value. Same format as TRecord.
func UnmarshalWValue(data []byte) ([]uint64, error) {
	return UnmarshalTValue(data) // identical format
}

// --- Key construction ---

// Record prefix bytes for the single subdatabase.
const (
	prefixC byte = 'C' // per-chunk record
	prefixF byte = 'F' // per-file record
	prefixH byte = 'H' // hash -> chunkid
	prefixI byte = 'I' // config (data-in-key)
	prefixN byte = 'N' // filename key chain
	prefixT byte = 'T' // trigram inverted index
	prefixW byte = 'W' // token inverted index
)

func makeCKey(chunkid uint64) []byte {
	buf := make([]byte, 1+binary.MaxVarintLen64)
	buf[0] = prefixC
	n := putUvarint(buf[1:], chunkid)
	return buf[:1+n]
}

func makeFKey(fileid uint64) []byte {
	buf := make([]byte, 1+binary.MaxVarintLen64)
	buf[0] = prefixF
	n := putUvarint(buf[1:], fileid)
	return buf[:1+n]
}

func makeHKey(hash [32]byte) []byte {
	key := make([]byte, 1+32)
	key[0] = prefixH
	copy(key[1:], hash[:])
	return key
}

func makeIKey(name string) []byte {
	key := make([]byte, 1+len(name))
	key[0] = prefixI
	copy(key[1:], name)
	return key
}

func makeTKey(trigram uint32) []byte {
	return []byte{prefixT, byte(trigram >> 16), byte(trigram >> 8), byte(trigram)}
}

func makeWKey(tokenHash uint32) []byte {
	return []byte{prefixW, byte(tokenHash >> 24), byte(tokenHash >> 16), byte(tokenHash >> 8), byte(tokenHash)}
}

