package microfts2

import (
	"encoding/binary"
	"fmt"

	"go.etcd.io/bbolt"
)

// TxnHolder carries a bbolt transaction's fts bucket.
// CRecord implements it; bucketWrap wraps raw buckets from View/Update blocks.
// CRC: crc-DB.md | R667, R734
type TxnHolder interface {
	Bucket() *bbolt.Bucket
}

// bucketWrap wraps a raw *bbolt.Bucket into a TxnHolder.
// CRC: crc-DB.md | R734
type bucketWrap struct{ b *bbolt.Bucket }

func (w bucketWrap) Bucket() *bbolt.Bucket { return w.b }

// TrigramEntry pairs a trigram code with its per-chunk occurrence count.
// CRC: crc-DB.md | R249
type TrigramEntry struct {
	Trigram uint32
	Count   int
}

// TokenEntry pairs a token string with its occurrence count.
// CRC: crc-DB.md | R250
type TokenEntry struct {
	Token string
	Count int
}

// FileChunkEntry is a per-occurrence entry in the F record's chunk list.
// Location is the chunker's Range (human display); Locator is the chunker's Locator
// (per-occurrence opaque bytes for fast retrieval and append-merge resume).
// Both are length-prefixed in the wire format and either may be empty.
// CRC: crc-DB.md | R251, R592
type FileChunkEntry struct {
	ChunkID  uint64
	Location string
	Locator  []byte
}

// FileIDCount records how many times a chunk occurs in a file.
// CRC: crc-DB.md | R595
type FileIDCount struct {
	FileID uint64
	Count  uint64
}

// CRecord is the per-chunk record. Self-describing: everything needed
// for search, scoring, filtering, and removal.
// Carries unexported db/bkt — the chunk is tied to the bucket that read it.
// CRC: crc-DB.md | R772, R496
type CRecord struct {
	ChunkID    uint64
	Hash       [32]byte
	ContentLen int
	// R772: no trigram or token list since format "6"; the per-trigram counts
	// live in the T postings
	DistinctTokens int // distinct tokens in the chunk: the length scoring normalizes by
	TokenTotal     int // summed token counts: what totalTokens adds and subtracts
	Attrs          []Pair
	FileIDs        []FileIDCount // R595: refcounted per-fileid occurrence count
	db             *DB
	bkt            *bbolt.Bucket
}

// FileIDCountFor returns the occurrence count for the given fileid, or 0 if absent.
func (c *CRecord) FileIDCountFor(fileid uint64) uint64 { return fileIDCount(c.FileIDs, fileid) }

// fileIDCount looks up fileid's count in a refcounted FileIDs slice.
func fileIDCount(s []FileIDCount, fileid uint64) uint64 {
	for _, fc := range s {
		if fc.FileID == fileid {
			return fc.Count
		}
	}
	return 0
}

// incFileID increments fileid's count (insert with count=1 if absent).
func incFileID(s []FileIDCount, fileid uint64) []FileIDCount {
	for i := range s {
		if s[i].FileID == fileid {
			s[i].Count++
			return s
		}
	}
	return append(s, FileIDCount{FileID: fileid, Count: 1})
}

// decFileID removes one occurrence of fileid; returns the slice and whether
// the entry was removed entirely (count hit 0).
func decFileID(s []FileIDCount, fileid uint64) ([]FileIDCount, bool) {
	for i := range s {
		if s[i].FileID == fileid {
			if s[i].Count > 1 {
				s[i].Count--
				return s, false
			}
			return append(s[:i], s[i+1:]...), true
		}
	}
	return s, false
}

// removeFileID drops all occurrences of fileid in one shot.
func removeFileID(s []FileIDCount, fileid uint64) ([]FileIDCount, bool) {
	for i := range s {
		if s[i].FileID == fileid {
			return append(s[:i], s[i+1:]...), true
		}
	}
	return s, false
}

// attach sets the db and bkt context for a CRecord.
// CRC: crc-DB.md | R664
func (c *CRecord) attach(db *DB, b *bbolt.Bucket) {
	c.db = db
	c.bkt = b
}

// Bucket returns the bbolt bucket this record was read from. Implements TxnHolder.
// CRC: crc-DB.md | R664, R734
func (c *CRecord) Bucket() *bbolt.Bucket { return c.bkt }

// Tx returns the bbolt transaction this record is part of.
// CRC: crc-DB.md | R664, R734
func (c *CRecord) Tx() *bbolt.Tx {
	if c.bkt == nil {
		return nil
	}
	return c.bkt.Tx()
}

// DB returns the database this record belongs to.
func (c *CRecord) DB() *DB { return c.db }

// FileRecord navigates to an F record within the same transaction.
// CRC: crc-DB.md | R266
func (c *CRecord) FileRecord(fileid uint64) (FRecord, error) {
	return c.db.readFRecord(c, fileid)
}

// IncFileID adds an occurrence for fileid.
// CRC: crc-DB.md | R596
func (c *CRecord) IncFileID(fileid uint64) { c.FileIDs = incFileID(c.FileIDs, fileid) }

// DecFileID removes one occurrence; returns true when the entry was removed entirely.
// CRC: crc-DB.md | R597
func (c *CRecord) DecFileID(fileid uint64) bool {
	var removed bool
	c.FileIDs, removed = decFileID(c.FileIDs, fileid)
	return removed
}

// RemoveFileID drops all occurrences of fileid in one shot.
// CRC: crc-DB.md | R599
func (c *CRecord) RemoveFileID(fileid uint64) bool {
	var removed bool
	c.FileIDs, removed = removeFileID(c.FileIDs, fileid)
	return removed
}

// FRecord is the per-file record. Metadata, ordered chunks, file-level token bag.
// CRC: crc-DB.md | R245
type FRecord struct {
	FileID      uint64
	ModTime     int64
	ContentHash [32]byte
	FileLength  int64
	Strategy    string
	// R717: the strategy's chunker version when the file was chunked
	ChunkerVersion uint32
	Name           string           // R766, R763: the file's path as last given
	Chunks         []FileChunkEntry // R766: the record ends here; no token bag since format "5"
}

// TRecord is the trigram inverted index entry: the trigram's postings, each a
// chunk with its count of the trigram.
// CRC: crc-DB.md | R246, R780
type TRecord struct {
	Trigram  uint32
	Postings []Posting
}

// Posting is one chunk in a T record, with how many times the trigram occurs
// in it.
// CRC: crc-DB.md | R780
type Posting struct {
	ChunkID uint64
	Count   int
}

// --- Encoding helpers ---

// R220: integers are varints
// R795: these LEB128 helpers are for values only; ids in keys use AppendKeyID
func putUvarint(buf []byte, v uint64) int { return binary.PutUvarint(buf, v) }
func readUvarint(buf []byte) (uint64, int) {
	v, n := binary.Uvarint(buf)
	return v, n
}

// R222: strings and byte slices are varint-length-prefixed
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
// Layout: hash + contentLen + distinctTokens + tokenTotal + attrs + [fileid, count] pairs.
// CRC: crc-DB.md | R221, R595, R735, R771
func (c *CRecord) MarshalValue() []byte {
	size := 32                        // hash
	size += 3 * binary.MaxVarintLen64 // contentLen, distinctTokens, tokenTotal
	size += binary.MaxVarintLen64     // n-attrs
	for _, p := range c.Attrs {
		size += binary.MaxVarintLen64 + len(p.Key) + binary.MaxVarintLen64 + len(p.Value)
	}
	size += binary.MaxVarintLen64                        // n-fileids
	size += len(c.FileIDs) * (2 * binary.MaxVarintLen64) // [fileid, count] pairs

	buf := make([]byte, size)
	off := 0

	copy(buf[off:], c.Hash[:])
	off += 32

	off += putUvarint(buf[off:], uint64(c.ContentLen))
	off += putUvarint(buf[off:], uint64(c.DistinctTokens))
	off += putUvarint(buf[off:], uint64(c.TokenTotal))

	off += putUvarint(buf[off:], uint64(len(c.Attrs)))
	for _, p := range c.Attrs {
		off += putBytes(buf[off:], p.Key)
		off += putBytes(buf[off:], p.Value)
	}

	// FileIDs as [fileid, count] pairs (R595)
	off += putUvarint(buf[off:], uint64(len(c.FileIDs)))
	for _, fc := range c.FileIDs {
		off += putUvarint(buf[off:], fc.FileID)
		off += putUvarint(buf[off:], fc.Count)
	}

	return buf[:off]
}

// UnmarshalCValue decodes a CRecord value. ChunkID must be set separately (from key).
// Layout: hash + contentLen + distinctTokens + tokenTotal + attrs + fileids.
// CRC: crc-DB.md | R221, R735, R771
func UnmarshalCValue(data []byte) (CRecord, error) {
	var c CRecord
	if len(data) < 32 {
		return c, fmt.Errorf("CRecord too short: %d bytes", len(data))
	}
	off := 0

	// Hash
	copy(c.Hash[:], data[off:off+32])
	off += 32

	// ContentLen (R771)
	cl, n := readUvarint(data[off:])
	if n <= 0 {
		return c, fmt.Errorf("CRecord: bad contentLen")
	}
	c.ContentLen = int(cl)
	off += n

	// R771: the token counts, then attrs; no trigram or token list
	dt, n := readUvarint(data[off:])
	if n <= 0 {
		return c, fmt.Errorf("CRecord: bad distinctTokens")
	}
	c.DistinctTokens = int(dt)
	off += n
	tt, n := readUvarint(data[off:])
	if n <= 0 {
		return c, fmt.Errorf("CRecord: bad tokenTotal")
	}
	c.TokenTotal = int(tt)
	off += n

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

	// FileIDs as [fileid, count] pairs (R595)
	nFIDs, n := readUvarint(data[off:])
	if n <= 0 {
		return c, fmt.Errorf("CRecord: bad n-fileids")
	}
	off += n
	c.FileIDs = make([]FileIDCount, nFIDs)
	for i := range c.FileIDs {
		fid, n := readUvarint(data[off:])
		if n <= 0 {
			return c, fmt.Errorf("CRecord: bad fileid")
		}
		c.FileIDs[i].FileID = fid
		off += n
		cnt, n := readUvarint(data[off:])
		if n <= 0 {
			return c, fmt.Errorf("CRecord: bad fileid count")
		}
		c.FileIDs[i].Count = cnt
		off += n
	}

	return c, nil
}

// --- FRecord marshal/unmarshal ---

// MarshalValue encodes the FRecord value (everything except the key prefix and fileid).
// Chunk entries are [chunkid, location, locator] — both location and locator are
// length-prefixed bytes (either may be empty).
// CRC: crc-DB.md | R592, R717, R735
func (f *FRecord) MarshalValue() []byte {
	size := 8 + 32                                  // modTime + contentHash
	size += binary.MaxVarintLen64                   // fileLength
	size += binary.MaxVarintLen64 + len(f.Strategy) // strategy
	size += binary.MaxVarintLen64                   // chunkerVersion
	size += binary.MaxVarintLen64 + len(f.Name)     // name (R765)
	size += binary.MaxVarintLen64                   // chunkcount
	for _, ch := range f.Chunks {
		size += binary.MaxVarintLen64                    // chunkid
		size += binary.MaxVarintLen64 + len(ch.Location) // location
		size += binary.MaxVarintLen64 + len(ch.Locator)  // locator
	}
	buf := make([]byte, size)
	off := 0

	binary.BigEndian.PutUint64(buf[off:], uint64(f.ModTime))
	off += 8

	copy(buf[off:], f.ContentHash[:])
	off += 32

	off += putUvarint(buf[off:], uint64(f.FileLength))

	off += putString(buf[off:], f.Strategy)

	off += putUvarint(buf[off:], uint64(f.ChunkerVersion)) // R717

	off += putString(buf[off:], f.Name) // R765: one name, no count

	// Chunks: [chunkid, location, locator]
	off += putUvarint(buf[off:], uint64(len(f.Chunks)))
	for _, ch := range f.Chunks {
		off += putUvarint(buf[off:], ch.ChunkID)
		off += putString(buf[off:], ch.Location)
		off += putBytes(buf[off:], ch.Locator)
	}

	return buf[:off] // R765: nothing after the chunk list
}

// unmarshalFHeader decodes the header fields of an F record value:
// ModTime, ContentHash, FileLength, Strategy, ChunkerVersion, and Name.
// Returns the record, the byte offset after Name, and any error.
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

	cv, n := readUvarint(data[off:]) // R717
	if n <= 0 {
		return f, 0, fmt.Errorf("FRecord: bad chunkerVersion")
	}
	f.ChunkerVersion = uint32(cv)
	off += n

	name, n := readString(data[off:]) // R765
	if n == 0 {
		return f, 0, fmt.Errorf("FRecord: bad name")
	}
	f.Name = name
	off += n

	return f, off, nil
}

// unmarshalFSummary decodes an F value's header and chunk count, stopping
// before the chunk entries and tokens.
// CRC: crc-DB.md | R750
func unmarshalFSummary(data []byte) (FileSummary, error) {
	f, off, err := unmarshalFHeader(data)
	if err != nil {
		return FileSummary{}, err
	}
	nChunks, n := readUvarint(data[off:])
	if n <= 0 {
		return FileSummary{}, fmt.Errorf("FRecord: bad chunkcount")
	}
	return FileSummary{ModTime: f.ModTime, FileLength: f.FileLength, Strategy: f.Strategy, ChunkCount: int(nChunks)}, nil
}

// UnmarshalFValue decodes an FRecord value. FileID must be set separately (from key).
// CRC: crc-DB.md | R592, R735
func UnmarshalFValue(data []byte) (FRecord, error) {
	f, off, err := unmarshalFHeader(data)
	if err != nil {
		return f, err
	}

	// R592: chunks are [chunkid, location, locator]
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
		locator, n := readBytes(data[off:])
		if n == 0 {
			return f, fmt.Errorf("FRecord: bad locator")
		}
		f.Chunks[i].Locator = locator
		off += n
	}

	// R765: the record ends after the chunk list. Bytes past it are an older
	// layout (a format "4" token bag) read as this one, so they are refused
	// rather than ignored.
	if off != len(data) {
		return f, fmt.Errorf("FRecord: %d bytes after the chunk list", len(data)-off)
	}
	return f, nil
}

// R451, R452: UnmarshalFHeader decodes only the header fields of an F record value:
// ModTime, ContentHash, FileLength, Strategy, ChunkerVersion, and Name. Skips Chunks.
func UnmarshalFHeader(data []byte) (FRecord, error) {
	f, _, err := unmarshalFHeader(data)
	return f, err
}

// --- TRecord marshal/unmarshal ---

// MarshalValue encodes the TRecord value: postings in ascending chunkid order,
// each `(delta << 1) | hasCount` as a uvarint, then the count only when it is
// not 1. The first delta is against 0.
// CRC: crc-DB.md | R735, R770, R780
func (t *TRecord) MarshalValue() []byte {
	return appendPostings(nil, 0, t.Postings)
}

// appendPostings encodes ps after buf, delta-coded against prev (the chunkid of
// the posting buf already ends with, or 0). ps must be ascending and above prev.
// CRC: crc-DB.md | R770
func appendPostings(buf []byte, prev uint64, ps []Posting) []byte {
	for _, p := range ps {
		delta := p.ChunkID - prev
		prev = p.ChunkID
		if p.Count == 1 {
			buf = binary.AppendUvarint(buf, delta<<1)
		} else {
			buf = binary.AppendUvarint(buf, delta<<1|1)
			buf = binary.AppendUvarint(buf, uint64(p.Count))
		}
	}
	return buf
}

// UnmarshalTValue decodes a TRecord value into its postings. Trigram must be set
// separately (from the key).
// CRC: crc-DB.md | R735, R770
func UnmarshalTValue(data []byte) ([]Posting, error) {
	var ps []Posting
	var prev uint64
	for off := 0; off < len(data); {
		v, n := readUvarint(data[off:])
		if n <= 0 {
			return nil, fmt.Errorf("TRecord: bad posting at offset %d", off)
		}
		off += n
		prev += v >> 1
		p := Posting{ChunkID: prev, Count: 1}
		if v&1 == 1 {
			c, n := readUvarint(data[off:])
			if n <= 0 {
				return nil, fmt.Errorf("TRecord: bad count at offset %d", off)
			}
			off += n
			p.Count = int(c)
		}
		ps = append(ps, p)
	}
	return ps, nil
}

// --- Key construction ---

// Record prefix bytes for the single bucket.
const (
	prefixC byte = 'C' // per-chunk record
	prefixF byte = 'F' // per-file record
	prefixH byte = 'H' // hash -> chunkid
	prefixI byte = 'I' // config (data-in-key)
	prefixN byte = 'N' // filename key chain
	prefixT byte = 'T' // trigram inverted index
	prefixD byte = 'D' // dead chunkid (R774); no W prefix since format "6" (R773)
)

// makeCKey, makeFKey and makeDKey hold their id in the order-preserving
// varint, so a new, larger id appends at the tree's right edge.
// CRC: crc-DB.md | R794
func makeCKey(chunkid uint64) []byte { return AppendKeyID([]byte{prefixC}, chunkid) }

func makeFKey(fileid uint64) []byte { return AppendKeyID([]byte{prefixF}, fileid) }

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

// makeDKey names a dead chunkid's D record (R774).
// CRC: crc-DB.md | R794
func makeDKey(chunkid uint64) []byte { return AppendKeyID([]byte{prefixD}, chunkid) }

// AppendKeyID appends v in SQLite4's order-preserving varint, the encoding
// of every id in a key: byte order is numeric order, and the first byte
// gives the length (1 byte to 240, 2 to 2287, 3 to 67823, then 247+n and v's
// n big-endian bytes, 3 <= n <= 8). Ids inside values stay LEB128 (R795).
// CRC: crc-DB.md | R791, R793
func AppendKeyID(b []byte, v uint64) []byte {
	switch {
	case v <= 240:
		return append(b, byte(v))
	case v <= 2287:
		x := v - 240
		return append(b, byte(241+x/256), byte(x%256))
	case v <= 67823:
		x := v - 2288
		return append(b, 249, byte(x/256), byte(x))
	}
	n := 3
	for n < 8 && v>>(8*uint(n)) != 0 {
		n++
	}
	b = append(b, byte(247+n))
	for i := n - 1; i >= 0; i-- {
		b = append(b, byte(v>>(8*uint(i))))
	}
	return b
}

// KeyID decodes one order-preserving varint from the front of b, returning
// its value and length. n == 0 means b is empty or too short, n < 0 that the
// encoding is not the shortest for its value; it never panics.
// CRC: crc-DB.md | R792, R793
func KeyID(b []byte) (v uint64, n int) {
	if len(b) == 0 {
		return 0, 0
	}
	switch a0 := b[0]; {
	case a0 <= 240:
		return uint64(a0), 1
	case a0 <= 248:
		if len(b) < 2 {
			return 0, 0
		}
		return 240 + 256*uint64(a0-241) + uint64(b[1]), 2
	case a0 == 249:
		if len(b) < 3 {
			return 0, 0
		}
		return 2288 + 256*uint64(b[1]) + uint64(b[2]), 3
	default:
		nb := int(a0) - 247
		if len(b) < 1+nb {
			return 0, 0
		}
		for _, c := range b[1 : 1+nb] {
			v = v<<8 | uint64(c)
		}
		if v <= 67823 || (nb > 3 && v>>(8*uint(nb-1)) == 0) {
			return 0, -1
		}
		return v, 1 + nb
	}
}
