package microfts2

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"regexp/syntax"

	csindex "github.com/google/codesearch/index"

	"errors"

	"github.com/bmatsuo/lmdb-go/lmdb"
)

// ErrNoChunks is returned when a chunker produces zero chunks for a file.
var ErrNoChunks = errors.New("chunker produced no chunks")

// ErrAlreadyIndexed is returned when AddFile is called for a path that already
// has F records in the database. Use Reindex or AppendChunks instead. R215
var ErrAlreadyIndexed = errors.New("file already indexed")

// CRC: crc-DB.md | Seq: seq-init.md, seq-add.md, seq-search.md, seq-score.md, seq-stale.md, seq-append.md, seq-chunks.md

// collectedChunk holds processed chunk data between generator collection and DB write.
type collectedChunk struct {
	rangeStr  string
	hash      [32]byte
	triCounts map[uint32]int
	tokens    []TokenEntry
}

// Chunk is a single chunk yielded by a ChunkFunc generator.
// Range is an opaque label (e.g. "1-10" for lines); Content is the chunk text.
// Both slices may be reused between yields — caller must copy if retaining.
type Chunk struct {
	Range   []byte
	Content []byte
}

// ChunkFunc is a generator that yields chunks for a file.
// It receives the file path and raw content, and calls yield for each chunk.
// If yield returns false, the generator should stop early.
type ChunkFunc func(path string, content []byte, yield func(Chunk) bool) error

type DB struct {
	env            *lmdb.Env
	dbi            lmdb.DBI
	dbName         string
	settings       Settings
	trigrams       *Trigrams
	funcStrategies map[string]ChunkFunc // in-memory Go function strategies
}

// Settings holds the in-memory representation of I records.
type Settings struct {
	CaseInsensitive    bool
	Aliases            map[byte]byte     // byte→byte alias mapping
	ChunkingStrategies map[string]string // name→cmd (empty cmd = func strategy)
}

// SearchResult is a single match from Search.
type SearchResult struct {
	Path  string
	Range string
	Score float64
}

// ScoredChunk is a per-chunk trigram match score from ScoreFile.
type ScoredChunk struct {
	Range string
	Score float64
}

// ChunkResult holds a single chunk with its content and position.
// R201
type ChunkResult struct {
	Path    string `json:"path"`
	Range   string `json:"range"`
	Content string `json:"content"`
	Index   int    `json:"index"` // 0-based position in the file's chunk list
}

// SearchResults wraps search matches with index health status.
type SearchResults struct {
	Results []SearchResult
	Status  IndexStatus
}

// IndexStatus reports the state of the index.
type IndexStatus struct {
	Built bool
}

// ScoreFunc computes a relevance score for a chunk.
// queryTrigrams: active query trigrams.
// chunkCounts: trigram -> occurrence count in the chunk.
// chunkTokenCount: number of tokens (words) in the chunk.
type ScoreFunc func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64

// TrigramCount pairs a trigram code with its corpus document frequency.
type TrigramCount struct {
	Trigram uint32
	Count   int
}

// TrigramFilter decides which trigrams to use for a given query.
// It receives the query's trigrams with their corpus-wide document frequencies,
// and the total number of indexed chunks. It returns the subset to search with.
type TrigramFilter func(trigrams []TrigramCount, totalChunks int) []TrigramCount

// SearchOption configures search behavior.
type SearchOption func(*searchConfig)

type searchConfig struct {
	scoreFunc          ScoreFunc
	onlyIDs            map[uint64]struct{} // if non-nil, only include these file IDs
	exceptIDs          map[uint64]struct{} // if non-nil, exclude these file IDs
	verify             bool                // post-filter: verify query terms in chunk text
	trigramFilter      TrigramFilter       // if non-nil, caller-supplied trigram selection
	regexFilters       []string            // AND: every pattern must match chunk content R183
	exceptRegexFilters []string            // subtract: any match rejects chunk R184
	chunkFilters       []ChunkFilter       // AND: all must pass for chunk to be included R255-R257
	proximityTopN      int                 // if > 0, rerank top-N by proximity R279
}

// ChunkFilter receives a CRecord during candidate evaluation.
// Return true to keep the chunk, false to reject it.
// The CRecord carries transaction context — use Txn() and DB() for lookups.
type ChunkFilter func(chunk CRecord) bool

// WithChunkFilter adds a chunk filter. Multiple calls accumulate (AND semantics).
func WithChunkFilter(fn ChunkFilter) SearchOption {
	return func(c *searchConfig) { c.chunkFilters = append(c.chunkFilters, fn) }
}

// WithCoverage uses coverage scoring (default): matching / total active query trigrams.
func WithCoverage() SearchOption {
	return func(c *searchConfig) { c.scoreFunc = scoreCoverage }
}

// WithDensity uses token-density scoring for long queries.
func WithDensity() SearchOption {
	return func(c *searchConfig) { c.scoreFunc = scoreDensity }
}

// CRC: crc-DB.md | R271
// WithOverlap uses overlap scoring: matching trigram count, no normalization.
func WithOverlap() SearchOption {
	return func(c *searchConfig) { c.scoreFunc = ScoreOverlap }
}

// WithScoring uses a custom scoring function.
func WithScoring(fn ScoreFunc) SearchOption {
	return func(c *searchConfig) { c.scoreFunc = fn }
}

// WithOnly restricts search to chunks from the given file IDs.
func WithOnly(ids map[uint64]struct{}) SearchOption {
	return func(c *searchConfig) { c.onlyIDs = ids }
}

// WithExcept excludes chunks from the given file IDs.
func WithExcept(ids map[uint64]struct{}) SearchOption {
	return func(c *searchConfig) { c.exceptIDs = ids }
}

// WithVerify enables post-filter verification: after trigram intersection,
// read chunk text from disk and verify each query term appears as a
// case-insensitive substring. Eliminates trigram false positives.
// R124, R125
func WithVerify() SearchOption {
	return func(c *searchConfig) { c.verify = true }
}

// WithTrigramFilter supplies a caller-defined trigram selection function.
func WithTrigramFilter(fn TrigramFilter) SearchOption {
	return func(c *searchConfig) { c.trigramFilter = fn }
}

// WithRegexFilter adds AND post-filters: every pattern must match chunk content.
// Multiple calls accumulate patterns. R183, R185
func WithRegexFilter(patterns ...string) SearchOption {
	return func(c *searchConfig) { c.regexFilters = append(c.regexFilters, patterns...) }
}

// WithExceptRegex adds subtract post-filters: any match rejects the chunk.
// Multiple calls accumulate patterns. R184, R185
func WithExceptRegex(patterns ...string) SearchOption {
	return func(c *searchConfig) { c.exceptRegexFilters = append(c.exceptRegexFilters, patterns...) }
}

// CRC: crc-DB.md | R279
// WithProximityRerank reranks the top-N results by query term proximity in chunk text.
func WithProximityRerank(topN int) SearchOption {
	return func(c *searchConfig) { c.proximityTopN = topN }
}

// FilterAll uses every query trigram. No filtering.
func FilterAll(trigrams []TrigramCount, _ int) []TrigramCount {
	return trigrams
}

// FilterByRatio returns a TrigramFilter that skips trigrams appearing in more
// than maxRatio of total chunks. E.g., 0.50 skips trigrams in >50% of chunks.
func FilterByRatio(maxRatio float64) TrigramFilter {
	return func(trigrams []TrigramCount, totalChunks int) []TrigramCount {
		threshold := int(float64(totalChunks) * maxRatio)
		var keep []TrigramCount
		for _, t := range trigrams {
			if t.Count <= threshold {
				keep = append(keep, t)
			}
		}
		return keep
	}
}

// FilterBestN returns a TrigramFilter that keeps the N trigrams with the lowest
// document frequency.
func FilterBestN(n int) TrigramFilter {
	return func(trigrams []TrigramCount, _ int) []TrigramCount {
		sorted := slices.Clone(trigrams)
		slices.SortFunc(sorted, func(a, b TrigramCount) int {
			return cmp.Compare(a.Count, b.Count)
		})
		if len(sorted) > n {
			sorted = sorted[:n]
		}
		return sorted
	}
}

func defaultSearchConfig() searchConfig {
	return searchConfig{scoreFunc: scoreCoverage}
}

func applySearchOpts(opts []SearchOption) searchConfig {
	cfg := defaultSearchConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ScoreCoverage is the coverage scoring function: fraction of active query trigrams present in chunk.
var ScoreCoverage ScoreFunc = scoreCoverage

// ScoreDensityFunc is the density scoring function for direct use with ScoreFile.
var ScoreDensityFunc ScoreFunc = scoreDensity

func scoreCoverage(queryTrigrams []uint32, chunkCounts map[uint32]int, _ int) float64 {
	if len(queryTrigrams) == 0 {
		return 0
	}
	matching := 0
	for _, tri := range queryTrigrams {
		if chunkCounts[tri] > 0 {
			matching++
		}
	}
	return float64(matching) / float64(len(queryTrigrams))
}

// scoreDensity: token-density scoring for long queries.
func scoreDensity(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64 {
	if chunkTokenCount == 0 {
		return 0
	}
	totalStrength := 0
	for _, tri := range queryTrigrams {
		c := chunkCounts[tri]
		if c > 0 {
			totalStrength += c
		}
	}
	return float64(totalStrength) / float64(chunkTokenCount)
}

// CRC: crc-DB.md | R269, R270
// ScoreOverlap: count of matching query trigrams, no normalization (OR semantics).
func ScoreOverlap(queryTrigrams []uint32, chunkCounts map[uint32]int, _ int) float64 {
	matching := 0
	for _, tri := range queryTrigrams {
		if chunkCounts[tri] > 0 {
			matching++
		}
	}
	return float64(matching)
}

// CRC: crc-DB.md | R272, R273
// ScoreBM25 returns a ScoreFunc closure implementing BM25 ranking.
// idf maps trigram codes to their inverse document frequency.
// avgTokenCount is the average chunk token count across the corpus.
func ScoreBM25(idf map[uint32]float64, avgTokenCount float64) ScoreFunc {
	const k1 = 1.2
	const b = 0.75
	return func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64 {
		dl := float64(chunkTokenCount)
		var score float64
		for _, tri := range queryTrigrams {
			tf := float64(chunkCounts[tri])
			if tf == 0 {
				continue
			}
			idfVal := idf[tri]
			score += idfVal * (tf * (k1 + 1)) / (tf + k1*(1-b+b*dl/avgTokenCount))
		}
		return score
	}
}

// CRC: crc-DB.md | R274, R277, R278
// BM25Func reads T records for per-trigram document frequencies and I record
// counters for corpus statistics, then returns a BM25 ScoreFunc closure.
func (db *DB) BM25Func(queryTrigrams []uint32) (ScoreFunc, error) {
	var scoreFn ScoreFunc
	err := db.env.View(func(txn *lmdb.Txn) error {
		th := txnWrap{txn}

		totalChunks, err := iCounter(th, db.dbi, "totalChunks")
		if err != nil {
			return err
		}
		totalTokens, err := iCounter(th, db.dbi, "totalTokens")
		if err != nil {
			return err
		}

		var avgdl float64
		if totalChunks > 0 {
			avgdl = float64(totalTokens) / float64(totalChunks)
		}

		n := float64(totalChunks)
		idfMap := make(map[uint32]float64, len(queryTrigrams))
		for _, tri := range queryTrigrams {
			var df int
			if tVal, err := txn.Get(db.dbi, makeTKey(tri)); err == nil {
				df = countTValue(tVal)
			}
			dfF := float64(df)
			idfMap[tri] = math.Log((n-dfF+0.5)/(dfF+0.5) + 1)
		}

		scoreFn = ScoreBM25(idfMap, avgdl)
		return nil
	})
	return scoreFn, err
}

// FileStatus is returned by CheckFile and StaleFiles.
type FileStatus struct {
	Path     string
	Status   string // "fresh", "stale", "missing"
	FileID   uint64
	Strategy string
}

// Options configures database creation and opening.
type Options struct {
	CaseInsensitive bool
	Aliases         map[byte]byte // maps input bytes to replacement bytes before trigram extraction
	DBName          string        // subdatabase name, default "fts"
	MaxDBs          int           // LMDB max named databases, default 2
	MapSize         int64         // bytes, default 1GB
}

func (o *Options) dbNameOrDefault() string {
	if o.DBName != "" {
		return o.DBName
	}
	return "fts"
}

func (o *Options) maxDBs() int {
	if o.MaxDBs > 0 {
		return o.MaxDBs
	}
	return 2
}

func (o *Options) mapSize() int64 {
	if o.MapSize > 0 {
		return o.MapSize
	}
	return 1 << 30
}

// --- Key construction ---

// parseNFinalValue extracts the full filename and fileid from an N-255 record value.
func parseNFinalValue(val []byte) (string, uint64) {
	name, n := readString(val)
	fileid, _ := readUvarint(val[n:])
	return name, fileid
}

// lookupFileByPath resolves a file path to its fileid and FRecord.
func (db *DB) lookupFileByPath(th TxnHolder, fpath string) (uint64, FRecord, error) {
	txn := th.Txn()
	finalKey := FinalKey(fpath)
	val, err := txn.Get(db.dbi, finalKey)
	if lmdb.IsNotFound(err) {
		return 0, FRecord{}, fmt.Errorf("file not found: %s", fpath)
	} else if err != nil {
		return 0, FRecord{}, fmt.Errorf("lookup %s: %w", fpath, err)
	}
	_, fileid := parseNFinalValue(val)
	frec, err := db.readFRecord(th, fileid)
	if err != nil {
		return 0, FRecord{}, err
	}
	return fileid, frec, nil
}

// lookupTrigramCounts reads T records for query trigrams and returns document frequencies.
// DF is derived from the number of varint-encoded chunkids in the T record value.
func lookupTrigramCounts(th TxnHolder, dbi lmdb.DBI, queryTrigrams []uint32) []TrigramCount {
	txn := th.Txn()
	seen := make(map[uint32]bool)
	var result []TrigramCount
	for _, t := range queryTrigrams {
		if seen[t] {
			continue
		}
		seen[t] = true
		key := makeTKey(t)
		var count int
		val, err := txn.Get(dbi, key)
		if err == nil {
			count = countTValue(val)
		}
		result = append(result, TrigramCount{Trigram: t, Count: count})
	}
	return result
}

// countTotalChunks scans F records and sums up the chunk count across all files.
func countTotalChunks(th TxnHolder, dbi lmdb.DBI) int {
	txn := th.Txn()
	total := 0
	cursor, err := txn.OpenCursor(dbi)
	if err != nil {
		return 0
	}
	defer cursor.Close()
	key, val, err := cursor.Get([]byte{prefixF}, nil, lmdb.SetRange)
	for err == nil && len(key) > 0 && key[0] == prefixF {
		data := make([]byte, len(val))
		copy(data, val)
		frec, fErr := UnmarshalFValue(data)
		if fErr == nil {
			total += len(frec.Chunks)
		}
		key, val, err = cursor.Get(nil, nil, lmdb.Next)
	}
	return total
}

// applyTrigramFilter uses the caller-supplied filter to select query trigrams.
// Returns the selected trigram codes as a []uint32.
func applyTrigramFilter(th TxnHolder, contentDBI lmdb.DBI, queryTrigrams []uint32, filter TrigramFilter) []uint32 {
	counts := lookupTrigramCounts(th, contentDBI, queryTrigrams)
	total := countTotalChunks(th, contentDBI)
	selected := filter(counts, total)
	result := make([]uint32, len(selected))
	for i, tc := range selected {
		result[i] = tc.Trigram
	}
	return result
}

// selectQueryTrigrams uses the caller-supplied filter (or FilterAll) to select query trigrams.
func selectQueryTrigrams(th TxnHolder, contentDBI lmdb.DBI, queryTrigrams []uint32, cfg searchConfig) []uint32 {
	filter := cfg.trigramFilter
	if filter == nil {
		filter = FilterAll
	}
	return applyTrigramFilter(th, contentDBI, queryTrigrams, filter)
}

// countTokens counts space-separated tokens in data.
func countTokens(data []byte) int {
	n := 0
	inWord := false
	for _, b := range data {
		if isWhitespace(b) {
			inWord = false
		} else if !inWord {
			inWord = true
			n++
		}
	}
	return n
}

// tokenizeCounts extracts space-separated tokens and returns their occurrence counts.
func tokenizeCounts(data []byte) []TokenEntry {
	counts := make(map[string]int)
	lower := bytes.ToLower(data)
	start := -1
	for i, b := range lower {
		if isWhitespace(b) {
			if start >= 0 {
				counts[string(lower[start:i])]++
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		counts[string(lower[start:])]++
	}
	entries := make([]TokenEntry, 0, len(counts))
	for tok, cnt := range counts {
		entries = append(entries, TokenEntry{Token: tok, Count: cnt})
	}
	return entries
}

// tokenHash computes a 4-byte hash of a token string for W record keys.
// Uses FNV-1a for good distribution.
func tokenHash(token string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(token); i++ {
		h ^= uint32(token[i])
		h *= 16777619
	}
	return h
}

// mergeTokenBag merges source token entries into a destination bag (summing counts).
func mergeTokenBag(dst map[string]int, src []TokenEntry) {
	for _, te := range src {
		dst[te.Token] += te.Count
	}
}

// tokenBagToEntries converts a map bag to a slice of TokenEntry.
func tokenBagToEntries(bag map[string]int) []TokenEntry {
	entries := make([]TokenEntry, 0, len(bag))
	for tok, cnt := range bag {
		entries = append(entries, TokenEntry{Token: tok, Count: cnt})
	}
	return entries
}

// --- T/W record helpers ---

// appendToInvertedRecord adds a chunkid to an inverted index record (read-modify-write).
// Works for both T and W records since they share the same varint-packed format.
func appendToInvertedRecord(th TxnHolder, dbi lmdb.DBI, key []byte, chunkid uint64) error {
	txn := th.Txn()
	var existing []byte
	val, err := txn.Get(dbi, key)
	if err == nil {
		existing = make([]byte, len(val))
		copy(existing, val)
	} else if !lmdb.IsNotFound(err) {
		return err
	}
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], chunkid)
	newVal := append(existing, buf[:n]...)
	return txn.Put(dbi, key, newVal, 0)
}

// appendChunkIDsToInvertedRecord appends multiple chunkids to an inverted index record.
func appendChunkIDsToInvertedRecord(th TxnHolder, dbi lmdb.DBI, key []byte, chunkids []uint64) error {
	txn := th.Txn()
	var existing []byte
	val, err := txn.Get(dbi, key)
	if err == nil {
		existing = make([]byte, len(val))
		copy(existing, val)
	} else if !lmdb.IsNotFound(err) {
		return err
	}
	extra := make([]byte, len(chunkids)*binary.MaxVarintLen64)
	off := 0
	for _, cid := range chunkids {
		off += binary.PutUvarint(extra[off:], cid)
	}
	newVal := append(existing, extra[:off]...)
	return txn.Put(dbi, key, newVal, 0)
}

// removeFromInvertedRecord removes a chunkid from an inverted index record value.
func removeFromInvertedRecord(th TxnHolder, dbi lmdb.DBI, key []byte, chunkid uint64) {
	txn := th.Txn()
	val, err := txn.Get(dbi, key)
	if err != nil {
		return
	}
	ids, _ := UnmarshalTValue(val)
	var newIDs []uint64
	for _, id := range ids {
		if id != chunkid {
			newIDs = append(newIDs, id)
		}
	}
	if len(newIDs) == 0 {
		txn.Del(dbi, key, nil)
	} else {
		txn.Put(dbi, key, marshalChunkIDs(newIDs), 0)
	}
}

// batchAppendT appends a chunkid to multiple T records.
func batchAppendT(th TxnHolder, dbi lmdb.DBI, trigrams []uint32, chunkid uint64) error {
	for _, tri := range trigrams {
		if err := appendToInvertedRecord(th, dbi, makeTKey(tri), chunkid); err != nil {
			return err
		}
	}
	return nil
}

// batchAppendW appends a chunkid to multiple W records.
func batchAppendW(th TxnHolder, dbi lmdb.DBI, tokens []TokenEntry, chunkid uint64) error {
	seen := make(map[uint32]bool)
	for _, te := range tokens {
		h := tokenHash(te.Token)
		if seen[h] {
			continue // same hash already appended in this batch
		}
		seen[h] = true
		if err := appendToInvertedRecord(th, dbi, makeWKey(h), chunkid); err != nil {
			return err
		}
	}
	return nil
}

// readFRecord reads an F record by fileid. TxnHolder-compatible.
func (db *DB) readFRecord(th TxnHolder, fileid uint64) (FRecord, error) {
	val, err := th.Txn().Get(db.dbi, makeFKey(fileid))
	if err != nil {
		return FRecord{}, fmt.Errorf("read F record %d: %w", fileid, err)
	}
	data := make([]byte, len(val))
	copy(data, val)
	f, err := UnmarshalFValue(data)
	if err != nil {
		return FRecord{}, err
	}
	f.FileID = fileid
	return f, nil
}

// --- I record helpers (data-in-key settings) ---

// iGet reads a single I record value. Returns ("", nil) if not found.
func iGet(th TxnHolder, dbi lmdb.DBI, name string) (string, error) {
	txn := th.Txn()
	val, err := txn.Get(dbi, makeIKey(name))
	if lmdb.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(val), nil
}

// iPut writes a single I record.
func iPut(th TxnHolder, dbi lmdb.DBI, name, value string) error {
	txn := th.Txn()
	return txn.Put(dbi, makeIKey(name), []byte(value), 0)
}

// iDel deletes a single I record.
func iDel(th TxnHolder, dbi lmdb.DBI, name string) error {
	txn := th.Txn()
	err := txn.Del(dbi, makeIKey(name), nil)
	if lmdb.IsNotFound(err) {
		return nil
	}
	return err
}

// iCounter reads a counter I record as uint64. Returns 0 if not found.
func iCounter(th TxnHolder, dbi lmdb.DBI, name string) (uint64, error) {
	txn := th.Txn()
	val, err := txn.Get(dbi, makeIKey(name))
	if lmdb.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if len(val) < 8 {
		return 0, fmt.Errorf("counter %q: short value (%d bytes)", name, len(val))
	}
	return binary.BigEndian.Uint64(val), nil
}

// iSetCounter writes a counter I record as 8-byte big-endian.
func iSetCounter(th TxnHolder, dbi lmdb.DBI, name string, v uint64) error {
	txn := th.Txn()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return txn.Put(dbi, makeIKey(name), buf[:], 0)
}

// CRC: crc-DB.md | R275, R276
// iAddCounter atomically adds delta to a counter I record.
func iAddCounter(th TxnHolder, dbi lmdb.DBI, name string, delta int64) error {
	cur, err := iCounter(th, dbi, name)
	if err != nil {
		return err
	}
	newVal := int64(cur) + delta
	if newVal < 0 {
		newVal = 0
	}
	return iSetCounter(th, dbi, name, uint64(newVal))
}

// updateCorpusCounters increments totalChunks and totalTokens for newly created chunks.
func updateCorpusCounters(th TxnHolder, dbi lmdb.DBI, newChunks []newChunkTW) error {
	if len(newChunks) == 0 {
		return nil
	}
	var totalNewTokens int64
	for _, nc := range newChunks {
		for _, te := range nc.tokens {
			totalNewTokens += int64(te.Count)
		}
	}
	if err := iAddCounter(th, dbi, "totalChunks", int64(len(newChunks))); err != nil {
		return err
	}
	return iAddCounter(th, dbi, "totalTokens", totalNewTokens)
}

// writeSettings writes all settings as individual I records.
func writeSettings(th TxnHolder, dbi lmdb.DBI, s *Settings) error {
	ci := "false"
	if s.CaseInsensitive {
		ci = "true"
	}
	if err := iPut(th, dbi, "caseInsensitive", ci); err != nil {
		return err
	}
	for from, to := range s.Aliases {
		key := fmt.Sprintf("alias:%c", from)
		if err := iPut(th, dbi, key, string([]byte{to})); err != nil {
			return err
		}
	}
	for name, cmd := range s.ChunkingStrategies {
		key := "strategy:" + name
		if err := iPut(th, dbi, key, cmd); err != nil {
			return err
		}
	}
	return nil
}

// loadSettings reads all settings from I records. Uses a cursor to scan the I prefix range.
func loadSettings(th TxnHolder, dbi lmdb.DBI) (Settings, error) {
	txn := th.Txn()
	s := Settings{
		ChunkingStrategies: make(map[string]string),
	}

	cursor, err := txn.OpenCursor(dbi)
	if err != nil {
		return s, err
	}
	defer cursor.Close()

	// Scan all I-prefixed keys
	startKey := []byte{prefixI}
	endKey := []byte{prefixI + 1}

	k, v, err := cursor.Get(startKey, nil, lmdb.SetRange)
	for err == nil {
		if len(k) < 1 || k[0] != prefixI || (len(k) > 0 && bytes.Compare(k, endKey) >= 0) {
			break
		}
		name := string(k[1:])
		value := string(v)

		switch {
		case name == "caseInsensitive":
			s.CaseInsensitive = (value == "true")
		case strings.HasPrefix(name, "alias:") && len(name) > 6:
			if s.Aliases == nil {
				s.Aliases = make(map[byte]byte)
			}
			from := name[6]
			if len(value) > 0 {
				s.Aliases[from] = value[0]
			}
		case strings.HasPrefix(name, "strategy:"):
			stratName := name[9:]
			s.ChunkingStrategies[stratName] = value
		}

		k, v, err = cursor.Get(nil, nil, lmdb.Next)
	}
	if err != nil && !lmdb.IsNotFound(err) {
		return s, err
	}

	return s, nil
}

// --- Create / Open / Close ---

// Seq: seq-init.md
func Create(path string, opts Options) (*DB, error) {
	// CRC: crc-DB.md | R115
	if err := ValidateAliases(opts.Aliases); err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	env, err := lmdb.NewEnv()
	if err != nil {
		return nil, fmt.Errorf("lmdb NewEnv: %w", err)
	}
	if err := env.SetMaxDBs(opts.maxDBs()); err != nil {
		env.Close()
		return nil, err
	}
	if err := env.SetMapSize(opts.mapSize()); err != nil {
		env.Close()
		return nil, err
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		env.Close()
		return nil, err
	}
	if err := env.Open(path, 0, 0644); err != nil {
		env.Close()
		return nil, fmt.Errorf("lmdb Open %s: %w", path, err)
	}

	settings := Settings{
		CaseInsensitive:    opts.CaseInsensitive,
		Aliases:            opts.Aliases,
		ChunkingStrategies: make(map[string]string),
	}

	dbName := opts.dbNameOrDefault()
	db := &DB{
		env:            env,
		dbName:         dbName,
		trigrams:       NewTrigrams(opts.CaseInsensitive, opts.Aliases),
		settings:       settings,
		funcStrategies: make(map[string]ChunkFunc),
	}

	err = env.Update(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI(dbName, lmdb.Create)
		if err != nil {
			return err
		}
		db.dbi = dbi

		th := txnWrap{txn}
		if err := writeSettings(th, dbi, &settings); err != nil {
			return err
		}
		// Initialize counters
		if err := iSetCounter(th, dbi, "nextFileID", 1); err != nil {
			return err
		}
		if err := iSetCounter(th, dbi, "nextChunkID", 1); err != nil {
			return err
		}
		if err := iSetCounter(th, dbi, "totalTokens", 0); err != nil {
			return err
		}
		if err := iSetCounter(th, dbi, "totalChunks", 0); err != nil {
			return err
		}
		return iPut(th, dbi, "version", "2")
	})
	if err != nil {
		env.Close()
		return nil, err
	}
	return db, nil
}

func Open(path string, opts Options) (*DB, error) {
	env, err := lmdb.NewEnv()
	if err != nil {
		return nil, fmt.Errorf("lmdb NewEnv: %w", err)
	}
	if err := env.SetMaxDBs(opts.maxDBs()); err != nil {
		env.Close()
		return nil, err
	}
	if err := env.SetMapSize(opts.mapSize()); err != nil {
		env.Close()
		return nil, err
	}
	if err := env.Open(path, 0, 0644); err != nil {
		env.Close()
		return nil, fmt.Errorf("lmdb Open %s: %w", path, err)
	}

	dbName := opts.dbNameOrDefault()
	db := &DB{
		env:            env,
		dbName:         dbName,
		funcStrategies: make(map[string]ChunkFunc),
	}

	err = env.Update(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI(dbName, 0)
		if err != nil {
			return fmt.Errorf("open db %q: %w", dbName, err)
		}
		db.dbi = dbi

		s, err := loadSettings(txnWrap{txn}, dbi)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		db.settings = s
		return nil
	})
	if err != nil {
		env.Close()
		return nil, err
	}

	db.trigrams = NewTrigrams(db.settings.CaseInsensitive, db.settings.Aliases)
	return db, nil
}

// Settings returns the current database settings.
func (db *DB) Settings() Settings {
	return db.settings
}

// QueryTrigramCounts extracts trigrams from a query string and returns
// their corpus document frequencies. For diagnostic/inspection use.
func (db *DB) QueryTrigramCounts(query string) ([]TrigramCount, error) {
	rawTrigrams := db.trigrams.ExtractTrigrams([]byte(query))
	if len(rawTrigrams) == 0 {
		return nil, nil
	}
	var result []TrigramCount
	err := db.env.View(func(txn *lmdb.Txn) error {
		result = lookupTrigramCounts(txnWrap{txn}, db.dbi, rawTrigrams)
		return nil
	})
	return result, err
}

func (db *DB) Close() error {
	if db.env != nil {
		db.env.Close()
		db.env = nil
	}
	return nil
}

// Env returns the underlying LMDB environment for sharing with other libraries.
func (db *DB) Env() *lmdb.Env {
	return db.env
}

// CRC: crc-DB.md
func (db *DB) Version() (string, error) {
	var version string
	err := db.env.View(func(txn *lmdb.Txn) error {
		var e error
		version, e = iGet(&txnWrap{txn}, db.dbi, "version")
		return e
	})
	return version, err
}

// --- AddFile ---

// Seq: seq-add.md
func (db *DB) AddFile(fpath, strategy string) (uint64, error) {
	fileid, _, err := db.addFileCore(fpath, strategy)
	return fileid, err
}

// CRC: crc-DB.md | R120
func (db *DB) AddFileWithContent(fpath, strategy string) (uint64, []byte, error) {
	return db.addFileCore(fpath, strategy)
}

// collectChunks reads a file, runs the chunker, and returns the collected chunks.
func (db *DB) collectChunks(fpath, strategy string) ([]collectedChunk, []byte, int64, [32]byte, error) {
	if _, ok := db.settings.ChunkingStrategies[strategy]; !ok {
		return nil, nil, 0, [32]byte{}, fmt.Errorf("unknown chunking strategy: %s", strategy)
	}

	modTime, err := fileModTime(fpath)
	if err != nil {
		return nil, nil, 0, [32]byte{}, err
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil, nil, 0, [32]byte{}, err
	}

	chunkFn := db.resolveChunkFunc(strategy)
	if chunkFn == nil {
		return nil, nil, 0, [32]byte{}, fmt.Errorf("func strategy %q not registered (re-register with AddStrategyFunc after Open)", strategy)
	}

	var chunks []collectedChunk
	var utf8Err error
	if err := chunkFn(fpath, data, func(c Chunk) bool {
		if !utf8.Valid(c.Content) {
			utf8Err = fmt.Errorf("chunk %q contains invalid UTF-8 in %s", c.Range, fpath)
			return false
		}
		h := sha256.Sum256(c.Content)
		chunks = append(chunks, collectedChunk{
			rangeStr:  string(c.Range),
			hash:      h,
			triCounts: db.trigrams.TrigramCounts(c.Content),
			tokens:    tokenizeCounts(c.Content),
		})
		return true
	}); err != nil {
		return nil, nil, 0, [32]byte{}, err
	}
	if utf8Err != nil {
		return nil, nil, 0, [32]byte{}, utf8Err
	}
	if len(chunks) == 0 {
		return nil, nil, 0, [32]byte{}, fmt.Errorf("%w: %s", ErrNoChunks, fpath)
	}

	return chunks, data, modTime, contentHash(data), nil
}

// Seq: seq-add.md | R118
func (db *DB) addFileCore(fpath, strategy string) (uint64, []byte, error) {
	chunks, data, modTime, hash, err := db.collectChunks(fpath, strategy)
	if err != nil {
		return 0, nil, err
	}

	var fileid uint64
	err = db.env.Update(func(txn *lmdb.Txn) error {
		var txnErr error
		fileid, txnErr = db.addFileInTxn(txnWrap{txn}, fpath, strategy, chunks, modTime, hash, int64(len(data)))
		return txnErr
	})
	return fileid, data, err
}

// resolveChunkFunc returns the ChunkFunc for a strategy, or nil if not available.
func (db *DB) resolveChunkFunc(strategy string) ChunkFunc {
	if fn, ok := db.funcStrategies[strategy]; ok {
		return fn
	}
	cmd := db.settings.ChunkingStrategies[strategy]
	if cmd == "" {
		return nil
	}
	return RunChunkerFunc(cmd)
}

// newChunkTW holds T/W batch data for a newly created chunk.
type newChunkTW struct {
	chunkid  uint64
	trigrams []uint32
	tokens   []TokenEntry
}

// dedupOrCreateChunk checks H record for dedup. On hit, adds fileid to existing C record.
// On miss, allocates new chunkid, creates H and C records. Returns chunkid and whether it was new.
func (db *DB) dedupOrCreateChunk(th TxnHolder, ch collectedChunk, fileid uint64) (uint64, *newChunkTW, error) {
	txn := th.Txn()
	hKey := makeHKey(ch.hash)

	hVal, err := txn.Get(db.dbi, hKey)
	if err == nil {
		// Dedup hit
		chunkid, _ := readUvarint(hVal)
		cKey := makeCKey(chunkid)
		cVal, err := txn.Get(db.dbi, cKey)
		if err != nil {
			return 0, nil, fmt.Errorf("read C record %d: %w", chunkid, err)
		}
		cData := make([]byte, len(cVal))
		copy(cData, cVal)
		crec, err := UnmarshalCValue(cData)
		if err != nil {
			return 0, nil, err
		}
		crec.FileIDs = append(crec.FileIDs, fileid)
		if err := txn.Put(db.dbi, cKey, crec.MarshalValue(), 0); err != nil {
			return 0, nil, err
		}
		return chunkid, nil, nil // nil newChunkTW = not new
	} else if !lmdb.IsNotFound(err) {
		return 0, nil, err
	}

	// New chunk
	chunkid, err := db.allocChunkID(th)
	if err != nil {
		return 0, nil, err
	}

	var hValBuf [binary.MaxVarintLen64]byte
	hn := binary.PutUvarint(hValBuf[:], chunkid)
	if err := txn.Put(db.dbi, hKey, hValBuf[:hn], 0); err != nil {
		return 0, nil, err
	}

	triEntries := make([]TrigramEntry, 0, len(ch.triCounts))
	triList := make([]uint32, 0, len(ch.triCounts))
	for tri, cnt := range ch.triCounts {
		triEntries = append(triEntries, TrigramEntry{Trigram: tri, Count: cnt})
		triList = append(triList, tri)
	}

	crec := CRecord{
		ChunkID:  chunkid,
		Hash:     ch.hash,
		Trigrams: triEntries,
		Tokens:   ch.tokens,
		FileIDs:  []uint64{fileid},
	}
	if err := txn.Put(db.dbi, makeCKey(chunkid), crec.MarshalValue(), 0); err != nil {
		return 0, nil, err
	}

	return chunkid, &newChunkTW{chunkid: chunkid, trigrams: triList, tokens: ch.tokens}, nil
}

// dedupOrCreateChunkIfAbsent is like dedupOrCreateChunk but only adds fileid
// if not already present in the C record (used by AppendChunks).
func (db *DB) dedupOrCreateChunkIfAbsent(th TxnHolder, ch collectedChunk, fileid uint64) (uint64, *newChunkTW, error) {
	txn := th.Txn()
	hKey := makeHKey(ch.hash)

	hVal, err := txn.Get(db.dbi, hKey)
	if err == nil {
		// Dedup hit
		chunkid, _ := readUvarint(hVal)
		cKey := makeCKey(chunkid)
		cVal, err := txn.Get(db.dbi, cKey)
		if err != nil {
			return 0, nil, fmt.Errorf("read C record %d: %w", chunkid, err)
		}
		cData := make([]byte, len(cVal))
		copy(cData, cVal)
		crec, err := UnmarshalCValue(cData)
		if err != nil {
			return 0, nil, err
		}
		// Only add fileid if not already present
		found := false
		for _, fid := range crec.FileIDs {
			if fid == fileid {
				found = true
				break
			}
		}
		if !found {
			crec.FileIDs = append(crec.FileIDs, fileid)
			if err := txn.Put(db.dbi, cKey, crec.MarshalValue(), 0); err != nil {
				return 0, nil, err
			}
		}
		return chunkid, nil, nil
	} else if !lmdb.IsNotFound(err) {
		return 0, nil, err
	}

	// New chunk — same as dedupOrCreateChunk
	chunkid, err := db.allocChunkID(th)
	if err != nil {
		return 0, nil, err
	}

	var hValBuf [binary.MaxVarintLen64]byte
	hn := binary.PutUvarint(hValBuf[:], chunkid)
	if err := txn.Put(db.dbi, hKey, hValBuf[:hn], 0); err != nil {
		return 0, nil, err
	}

	triEntries := make([]TrigramEntry, 0, len(ch.triCounts))
	triList := make([]uint32, 0, len(ch.triCounts))
	for tri, cnt := range ch.triCounts {
		triEntries = append(triEntries, TrigramEntry{Trigram: tri, Count: cnt})
		triList = append(triList, tri)
	}

	crec := CRecord{
		ChunkID:  chunkid,
		Hash:     ch.hash,
		Trigrams: triEntries,
		Tokens:   ch.tokens,
		FileIDs:  []uint64{fileid},
	}
	if err := txn.Put(db.dbi, makeCKey(chunkid), crec.MarshalValue(), 0); err != nil {
		return 0, nil, err
	}

	return chunkid, &newChunkTW{chunkid: chunkid, trigrams: triList, tokens: ch.tokens}, nil
}

// coalescedAppendT coalesces trigram→chunkids across all new chunks and does
// one read-modify-write per unique trigram.
func coalescedAppendT(th TxnHolder, dbi lmdb.DBI, newChunks []newChunkTW) error {
	triMap := make(map[uint32][]uint64)
	for _, nc := range newChunks {
		for _, tri := range nc.trigrams {
			triMap[tri] = append(triMap[tri], nc.chunkid)
		}
	}
	for tri, cids := range triMap {
		if err := appendChunkIDsToInvertedRecord(th, dbi, makeTKey(tri), cids); err != nil {
			return err
		}
	}
	return nil
}

// coalescedAppendW coalesces token hash→chunkids across all new chunks and does
// one read-modify-write per unique token hash.
func coalescedAppendW(th TxnHolder, dbi lmdb.DBI, newChunks []newChunkTW) error {
	wMap := make(map[uint32][]uint64)
	for _, nc := range newChunks {
		seen := make(map[uint32]bool)
		for _, te := range nc.tokens {
			h := tokenHash(te.Token)
			if seen[h] {
				continue
			}
			seen[h] = true
			wMap[h] = append(wMap[h], nc.chunkid)
		}
	}
	for h, cids := range wMap {
		if err := appendChunkIDsToInvertedRecord(th, dbi, makeWKey(h), cids); err != nil {
			return err
		}
	}
	return nil
}

// Seq: seq-add.md | R213, R214, R223-R226, R233-R240, R253
func (db *DB) addFileInTxn(th TxnHolder, fpath, strategy string, chunks []collectedChunk, modTime int64, hash [32]byte, fileLength int64) (uint64, error) {
	txn := th.Txn()
	// Dedup guard: check for existing N records before allocating a fileid
	finalKey := FinalKey(fpath)
	_, err := txn.Get(db.dbi, finalKey)
	if err == nil {
		return 0, ErrAlreadyIndexed
	} else if !lmdb.IsNotFound(err) {
		return 0, fmt.Errorf("check existing %s: %w", fpath, err)
	}

	fileid, err := db.allocFileID(th)
	if err != nil {
		return 0, err
	}

	// Write N records (filename key chain)
	pairs := EncodeFilename(fpath)
	fileidBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(fileidBuf, fileid)
	for i, pair := range pairs {
		val := []byte{}
		if i == len(pairs)-1 {
			// Final key value: length-prefixed full name + fileid varint
			nameBytes := []byte(fpath)
			v := make([]byte, binary.MaxVarintLen64+len(nameBytes)+n)
			off := binary.PutUvarint(v, uint64(len(nameBytes)))
			off += copy(v[off:], nameBytes)
			off += copy(v[off:], fileidBuf[:n])
			val = v[:off]
		}
		if err := txn.Put(db.dbi, pair.Key, val, 0); err != nil {
			return 0, err
		}
	}

	// Process each chunk — chunk dedup via H records
	fileChunks := make([]FileChunkEntry, len(chunks))
	fileBag := make(map[string]int) // file-level token bag
	var newChunks []newChunkTW

	for i, ch := range chunks {
		chunkid, nc, err := db.dedupOrCreateChunk(th, ch, fileid)
		if err != nil {
			return 0, err
		}
		if nc != nil {
			newChunks = append(newChunks, *nc)
		}
		fileChunks[i] = FileChunkEntry{ChunkID: chunkid, Location: ch.rangeStr}
		mergeTokenBag(fileBag, ch.tokens)
	}

	// Coalesced T/W record updates — one read-modify-write per unique trigram/token hash
	if err := coalescedAppendT(th, db.dbi, newChunks); err != nil {
		return 0, err
	}
	if err := coalescedAppendW(th, db.dbi, newChunks); err != nil {
		return 0, err
	}

	// R275, R276: update corpus counters for new chunks
	if err := updateCorpusCounters(th, db.dbi, newChunks); err != nil {
		return 0, err
	}

	// Write F record
	frec := FRecord{
		FileID:      fileid,
		ModTime:     modTime,
		ContentHash: hash,
		FileLength:  fileLength,
		Strategy:    strategy,
		Names:       []string{fpath},
		Chunks:      fileChunks,
		Tokens:      tokenBagToEntries(fileBag),
	}
	return fileid, txn.Put(db.dbi, makeFKey(fileid), frec.MarshalValue(), 0)
}

// --- RemoveFile ---

func (db *DB) RemoveFile(fpath string) error {
	return db.env.Update(func(txn *lmdb.Txn) error {
		return db.removeFileInTxn(txnWrap{txn}, fpath)
	})
}

// R254: Remove via F record → C records → T/W cleanup for orphaned chunks
func (db *DB) removeFileInTxn(th TxnHolder, fpath string) error {
	txn := th.Txn()
	fileid, frec, err := db.lookupFileByPath(th, fpath)
	if err != nil {
		return err
	}

	// For each chunk: remove fileid from C record, clean up orphans
	var removedChunks int64
	var removedTokens int64
	for _, fce := range frec.Chunks {
		cKey := makeCKey(fce.ChunkID)
		cVal, err := txn.Get(db.dbi, cKey)
		if err != nil {
			continue // C record missing — skip
		}
		cData := make([]byte, len(cVal))
		copy(cData, cVal)
		crec, err := UnmarshalCValue(cData)
		if err != nil {
			continue
		}

		// Remove this fileid from the C record
		newFIDs := make([]uint64, 0, len(crec.FileIDs))
		for _, fid := range crec.FileIDs {
			if fid != fileid {
				newFIDs = append(newFIDs, fid)
			}
		}

		if len(newFIDs) > 0 {
			// Chunk still referenced by other files — update C record
			crec.FileIDs = newFIDs
			if err := txn.Put(db.dbi, cKey, crec.MarshalValue(), 0); err != nil {
				return err
			}
		} else {
			// Orphaned chunk — delete C, H, and remove from T/W records
			removedChunks++
			for _, te := range crec.Tokens {
				removedTokens += int64(te.Count)
			}
			txn.Del(db.dbi, cKey, nil)
			txn.Del(db.dbi, makeHKey(crec.Hash), nil)

			// Remove chunkid from T records
			for _, te := range crec.Trigrams {
				removeFromInvertedRecord(th, db.dbi, makeTKey(te.Trigram), fce.ChunkID)
			}
			// Remove chunkid from W records
			seen := make(map[uint32]bool)
			for _, te := range crec.Tokens {
				h := tokenHash(te.Token)
				if seen[h] {
					continue
				}
				seen[h] = true
				removeFromInvertedRecord(th, db.dbi, makeWKey(h), fce.ChunkID)
			}
		}
	}

	// R275, R276: decrement corpus counters for orphaned chunks
	if removedChunks > 0 {
		if err := iAddCounter(th, db.dbi, "totalChunks", -removedChunks); err != nil {
			return err
		}
		if err := iAddCounter(th, db.dbi, "totalTokens", -removedTokens); err != nil {
			return err
		}
	}

	// Delete F record
	txn.Del(db.dbi, makeFKey(fileid), nil)

	// Delete N records (key chain)
	for _, pair := range EncodeFilename(fpath) {
		txn.Del(db.dbi, pair.Key, nil)
	}
	return nil
}

// --- Reindex ---

func (db *DB) Reindex(fpath, strategy string) (uint64, error) {
	fileid, _, err := db.reindexCore(fpath, strategy)
	return fileid, err
}

// CRC: crc-DB.md | R121
func (db *DB) ReindexWithContent(fpath, strategy string) (uint64, []byte, error) {
	return db.reindexCore(fpath, strategy)
}

func (db *DB) reindexCore(fpath, strategy string) (uint64, []byte, error) {
	chunks, data, modTime, hash, err := db.collectChunks(fpath, strategy)
	if err != nil {
		return 0, nil, err
	}

	// Single transaction: remove old records then add new ones
	var fileid uint64
	err = db.env.Update(func(txn *lmdb.Txn) error {
		th := txnWrap{txn}
		if err := db.removeFileInTxn(th, fpath); err != nil {
			return err
		}
		var txnErr error
		fileid, txnErr = db.addFileInTxn(th, fpath, strategy, chunks, modTime, hash, int64(len(data)))
		return txnErr
	})
	return fileid, data, err
}

// --- AppendChunks ---

// AppendOption configures AppendChunks behavior.
// R158
type AppendOption func(*appendConfig)

type appendConfig struct {
	contentHash   string
	modTime       int64
	fileLength    int64
	hasFileLength bool
	baseLine      int
}

// WithContentHash sets the full-file SHA-256 hash (caller pre-computed).
// R159
func WithContentHash(hash string) AppendOption {
	return func(c *appendConfig) { c.contentHash = hash }
}

// WithModTime sets the file modification time (Unix nanoseconds).
// R160
func WithModTime(t int64) AppendOption {
	return func(c *appendConfig) { c.modTime = t }
}

// WithFileLength sets the full file size after append.
// R161
func WithFileLength(n int64) AppendOption {
	return func(c *appendConfig) { c.fileLength = n; c.hasFileLength = true }
}

// WithBaseLine sets the 1-based line number offset for line-based chunker ranges.
// When non-zero, "start-end" ranges are adjusted by adding this offset.
// R162
func WithBaseLine(n int) AppendOption {
	return func(c *appendConfig) { c.baseLine = n }
}

// AppendChunks adds chunks to an existing file without full reindex.
// content is only the appended bytes, not the full file.
// CRC: crc-DB.md | Seq: seq-append.md
// R150, R151, R152, R153, R154, R155, R156, R157, R163, R164, R165, R166, R167, R168
func (db *DB) AppendChunks(fileid uint64, content []byte, strategy string, opts ...AppendOption) error {
	var cfg appendConfig
	for _, o := range opts {
		o(&cfg)
	}

	// Resolve chunk function
	chunkFn := db.resolveChunkFunc(strategy)
	if chunkFn == nil {
		return fmt.Errorf("func strategy %q not registered", strategy)
	}

	// Read existing F record
	var frec FRecord
	err := db.env.View(func(txn *lmdb.Txn) error {
		var err error
		frec, err = db.readFRecord(txnWrap{txn}, fileid)
		return err
	})
	if err != nil {
		return fmt.Errorf("fileid %d not found: %w", fileid, err)
	}

	filename := ""
	if len(frec.Names) > 0 {
		filename = frec.Names[0]
	}

	// Chunk the appended content
	var newChunks []collectedChunk
	var utf8Err error
	if err := chunkFn(filename, content, func(c Chunk) bool {
		if !utf8.Valid(c.Content) {
			utf8Err = fmt.Errorf("chunk %q contains invalid UTF-8", c.Range)
			return false
		}
		h := sha256.Sum256(c.Content)
		newChunks = append(newChunks, collectedChunk{
			rangeStr:  string(c.Range),
			hash:      h,
			triCounts: db.trigrams.TrigramCounts(c.Content),
			tokens:    tokenizeCounts(c.Content),
		})
		return true
	}); err != nil {
		return err
	}
	if utf8Err != nil {
		return utf8Err
	}
	if len(newChunks) == 0 {
		return nil // nothing to append
	}

	// Adjust ranges if baseLine is set (R165, R166)
	if cfg.baseLine > 0 {
		for i := range newChunks {
			adjusted, err := adjustRange(newChunks[i].rangeStr, cfg.baseLine)
			if err != nil {
				return fmt.Errorf("adjust range %q: %w", newChunks[i].rangeStr, err)
			}
			newChunks[i].rangeStr = adjusted
		}
	}

	// Single atomic write transaction (R164)
	return db.env.Update(func(txn *lmdb.Txn) error {
		th := txnWrap{txn}
		// Re-read F record inside write txn for consistency
		frec, err := db.readFRecord(th, fileid)
		if err != nil {
			return err
		}

		// Accumulate new file-level token bag from existing
		fileBag := make(map[string]int)
		mergeTokenBag(fileBag, frec.Tokens)

		var newChunksTW []newChunkTW

		for _, ch := range newChunks {
			chunkid, nc, err := db.dedupOrCreateChunkIfAbsent(th, ch, fileid)
			if err != nil {
				return err
			}
			if nc != nil {
				newChunksTW = append(newChunksTW, *nc)
			}
			frec.Chunks = append(frec.Chunks, FileChunkEntry{ChunkID: chunkid, Location: ch.rangeStr})
			mergeTokenBag(fileBag, ch.tokens)
		}

		// Coalesced T/W record updates
		if err := coalescedAppendT(th, db.dbi, newChunksTW); err != nil {
			return err
		}
		if err := coalescedAppendW(th, db.dbi, newChunksTW); err != nil {
			return err
		}

		// R275, R276: update corpus counters for new chunks
		if err := updateCorpusCounters(th, db.dbi, newChunksTW); err != nil {
			return err
		}

		// Update F record metadata
		if cfg.contentHash != "" {
			hb, _ := hex.DecodeString(cfg.contentHash)
			copy(frec.ContentHash[:], hb)
		}
		if cfg.modTime != 0 {
			frec.ModTime = cfg.modTime
		}
		if cfg.hasFileLength {
			frec.FileLength = cfg.fileLength
		}
		frec.Tokens = tokenBagToEntries(fileBag)

		return txn.Put(db.dbi, makeFKey(fileid), frec.MarshalValue(), 0)
	})
}

// adjustRange parses a "start-end" range string and adds baseLine to both values.
// R166
func adjustRange(rangeStr string, baseLine int) (string, error) {
	idx := strings.IndexByte(rangeStr, '-')
	if idx < 1 || idx == len(rangeStr)-1 {
		return rangeStr, nil // not a valid range, return as-is
	}
	start, errS := strconv.Atoi(rangeStr[:idx])
	end, errE := strconv.Atoi(rangeStr[idx+1:])
	if errS != nil || errE != nil {
		return rangeStr, nil // not numeric, return as-is
	}
	return strconv.Itoa(start+baseLine) + "-" + strconv.Itoa(end+baseLine), nil
}

// --- Search ---

// searchPrepare extracts per-term trigrams and the union set from parsed query terms.
func (db *DB) searchPrepare(terms []string) (termTrigrams [][]uint32, queryTrigrams []uint32) {
	termTrigrams = make([][]uint32, len(terms))
	trigramSet := make(map[uint32]bool)
	for i, term := range terms {
		tris := db.trigrams.ExtractTrigrams([]byte(term))
		termTrigrams[i] = tris
		for _, t := range tris {
			trigramSet[t] = true
		}
	}
	queryTrigrams = make([]uint32, 0, len(trigramSet))
	for t := range trigramSet {
		queryTrigrams = append(queryTrigrams, t)
	}
	return
}

// searchCollect selects trigrams, reads T records, intersects per-term candidate sets,
// and loads C records. Returns candidates and active trigrams.
func (db *DB) searchCollect(th TxnHolder, termTrigrams [][]uint32, queryTrigrams []uint32, cfg searchConfig) ([]candidateChunk, []uint32) {
	active := selectQueryTrigrams(th, db.dbi, queryTrigrams, cfg)
	if len(active) == 0 {
		return nil, nil
	}
	activeSet := make(map[uint32]bool, len(active))
	for _, t := range active {
		activeSet[t] = true
	}

	var candidateChunkIDs map[uint64]bool
	for _, tris := range termTrigrams {
		var termActive []uint32
		for _, t := range tris {
			if activeSet[t] {
				termActive = append(termActive, t)
			}
		}
		if len(termActive) == 0 {
			continue
		}
		termChunks := collectChunkIDs(th, db.dbi, termActive)
		if candidateChunkIDs == nil {
			candidateChunkIDs = termChunks
		} else {
			candidateChunkIDs = intersectChunkSets(candidateChunkIDs, termChunks)
		}
		if len(candidateChunkIDs) == 0 {
			return nil, nil
		}
	}

	cands := db.collectCandidates(th, candidateChunkIDs, cfg)
	return cands, active
}

// Seq: seq-search.md | R178, R179, R180, R181, R182
func (db *DB) Search(query string, opts ...SearchOption) (*SearchResults, error) {
	cfg := applySearchOpts(opts)

	query = strings.TrimSpace(query)
	terms := parseQueryTerms(query)
	if len(terms) == 0 {
		return &SearchResults{Status: IndexStatus{Built: true}}, nil
	}

	termTrigrams, queryTrigrams := db.searchPrepare(terms)
	if len(queryTrigrams) == 0 {
		return &SearchResults{Status: IndexStatus{Built: true}}, nil
	}

	var results []SearchResult

	err := db.env.View(func(txn *lmdb.Txn) error {
		cands, active := db.searchCollect(txnWrap{txn}, termTrigrams, queryTrigrams, cfg)
		if len(cands) == 0 {
			return nil
		}
		results = db.scoreAndResolve(txnWrap{txn}, cands, active, cfg.scoreFunc, cfg)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if cfg.verify {
		results = verifyResults(db, results, query)
	}

	// R188, R189: apply regex post-filters after verify, before sort
	results, err = applyRegexPostFilters(db, results, cfg)
	if err != nil {
		return nil, err
	}

	// R279: proximity rerank if configured
	if cfg.proximityTopN > 0 {
		results = proximityRerank(db, results, query, cfg.proximityTopN)
	}

	sortResults(results)
	return &SearchResults{
		Results: results,
		Status:  IndexStatus{Built: true},
	}, nil
}

// CRC: crc-DB.md | R286
// MultiSearchResult holds one strategy's results from SearchMulti.
type MultiSearchResult struct {
	Strategy string
	Results  []SearchResult
}

// CRC: crc-DB.md | Seq: seq-search-multi.md | R283, R284, R285, R287, R288, R289, R290
func (db *DB) SearchMulti(query string, strategies map[string]ScoreFunc, k int, opts ...SearchOption) ([]MultiSearchResult, error) {
	cfg := applySearchOpts(opts)

	query = strings.TrimSpace(query)
	terms := parseQueryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}

	termTrigrams, queryTrigrams := db.searchPrepare(terms)
	if len(queryTrigrams) == 0 {
		return nil, nil
	}

	var multiResults []MultiSearchResult

	err := db.env.View(func(txn *lmdb.Txn) error {
		cands, active := db.searchCollect(txnWrap{txn}, termTrigrams, queryTrigrams, cfg)
		if len(cands) == 0 {
			return nil
		}

		// Score with each strategy
		for name, scoreFn := range strategies {
			results := db.scoreAndResolve(txnWrap{txn}, cands, active, scoreFn, cfg)
			sortResults(results)
			if k > 0 && len(results) > k {
				results = results[:k]
			}
			multiResults = append(multiResults, MultiSearchResult{
				Strategy: name,
				Results:  results,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Apply post-filters per strategy
	for i := range multiResults {
		r := multiResults[i].Results
		if cfg.verify {
			r = verifyResults(db, r, query)
		}
		var ferr error
		r, ferr = applyRegexPostFilters(db, r, cfg)
		if ferr != nil {
			return nil, ferr
		}
		if cfg.proximityTopN > 0 {
			r = proximityRerank(db, r, query, cfg.proximityTopN)
		}
		sortResults(r)
		multiResults[i].Results = r
	}

	// Sort strategies by name for deterministic output
	slices.SortFunc(multiResults, func(a, b MultiSearchResult) int {
		return cmp.Compare(a.Strategy, b.Strategy)
	})

	return multiResults, nil
}

// collectChunkIDs reads T records for each trigram and returns the intersection of chunkid sets.
func collectChunkIDs(th TxnHolder, dbi lmdb.DBI, trigrams []uint32) map[uint64]bool {
	txn := th.Txn()
	if len(trigrams) == 0 {
		return nil
	}
	// Read first trigram's T record
	val, err := txn.Get(dbi, makeTKey(trigrams[0]))
	if err != nil {
		return nil
	}
	ids, _ := UnmarshalTValue(val)
	result := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}

	// Intersect with remaining trigrams
	for _, tri := range trigrams[1:] {
		if len(result) == 0 {
			return nil
		}
		val, err := txn.Get(dbi, makeTKey(tri))
		if err != nil {
			return nil // trigram not in index → empty intersection
		}
		ids, _ := UnmarshalTValue(val)
		next := make(map[uint64]bool, len(ids))
		for _, id := range ids {
			if result[id] {
				next[id] = true
			}
		}
		result = next
	}
	return result
}

// intersectChunkSets returns the intersection of two chunkid sets.
func intersectChunkSets(a, b map[uint64]bool) map[uint64]bool {
	// Iterate over the smaller set
	if len(a) > len(b) {
		a, b = b, a
	}
	result := make(map[uint64]bool, len(a))
	for id := range a {
		if b[id] {
			result[id] = true
		}
	}
	return result
}

// candidateChunk holds pre-loaded chunk data for scoring. CRC: crc-DB.md | R284
type candidateChunk struct {
	chunkID     uint64
	crec        CRecord
	chunkCounts map[uint32]int
	tokenCount  int
}

// collectCandidates reads C records for candidate chunkids, applies chunk filters,
// and returns pre-loaded candidates. CRC: crc-DB.md | R284
func (db *DB) collectCandidates(th TxnHolder, chunkIDs map[uint64]bool, cfg searchConfig) []candidateChunk {
	txn := th.Txn()
	var candidates []candidateChunk
	for chunkid := range chunkIDs {
		cVal, err := txn.Get(db.dbi, makeCKey(chunkid))
		if err != nil {
			continue
		}
		cData := make([]byte, len(cVal))
		copy(cData, cVal)
		crec, err := UnmarshalCValue(cData)
		if err != nil {
			continue
		}
		crec.ChunkID = chunkid
		crec.attach(db, txn)

		if !applyChunkFilters(crec, cfg) {
			continue
		}

		chunkCounts := make(map[uint32]int, len(crec.Trigrams))
		for _, te := range crec.Trigrams {
			chunkCounts[te.Trigram] = te.Count
		}

		candidates = append(candidates, candidateChunk{
			chunkID:     chunkid,
			crec:        crec,
			chunkCounts: chunkCounts,
			tokenCount:  len(crec.Tokens),
		})
	}
	return candidates
}

// scoreAndResolve scores pre-loaded candidates and resolves to SearchResults.
func (db *DB) scoreAndResolve(th TxnHolder, candidates []candidateChunk, active []uint32, scoreFunc ScoreFunc, cfg searchConfig) []SearchResult {
	txn := th.Txn()
	var results []SearchResult
	frecCache := make(map[uint64]*FRecord)

	for _, cand := range candidates {
		var score float64
		if active == nil {
			score = 1.0
		} else {
			score = scoreFunc(active, cand.chunkCounts, cand.tokenCount)
			if score <= 0 {
				continue
			}
		}

		for _, fid := range cand.crec.FileIDs {
			if cfg.onlyIDs != nil {
				if _, ok := cfg.onlyIDs[fid]; !ok {
					continue
				}
			}
			if cfg.exceptIDs != nil {
				if _, ok := cfg.exceptIDs[fid]; ok {
					continue
				}
			}

			frec, ok := frecCache[fid]
			if !ok {
				f, err := db.readFRecord(txnWrap{txn}, fid)
				if err != nil {
					continue
				}
				frec = &f
				frecCache[fid] = frec
			}

			for _, fce := range frec.Chunks {
				if fce.ChunkID == cand.chunkID {
					path := ""
					if len(frec.Names) > 0 {
						path = frec.Names[0]
					}
					results = append(results, SearchResult{
						Path:  path,
						Range: fce.Location,
						Score: score,
					})
					break
				}
			}
		}
	}
	return results
}

// applyChunkFilters runs all ChunkFilter functions, returning false if any rejects.
func applyChunkFilters(crec CRecord, cfg searchConfig) bool {
	for _, f := range cfg.chunkFilters {
		if !f(crec) {
			return false
		}
	}
	return true
}

// parseQueryTerms splits a query into terms: space-delimited words,
// with double-quoted phrases treated as single terms (quotes stripped).
// R125
func parseQueryTerms(query string) []string {
	var terms []string
	s := query
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ")
		if len(s) == 0 {
			break
		}
		if s[0] == '"' {
			end := strings.IndexByte(s[1:], '"')
			if end >= 0 {
				terms = append(terms, s[1:1+end])
				s = s[2+end:]
				continue
			}
		}
		end := strings.IndexByte(s, ' ')
		if end < 0 {
			terms = append(terms, s)
			break
		}
		terms = append(terms, s[:end])
		s = s[end+1:]
	}
	return terms
}

// extractPerTermTrigrams parses the query into terms and returns the deduplicated
// union of all terms' trigrams. Used by ScoreFile for per-token trigram generation.
func extractPerTermTrigrams(db *DB, query string) []uint32 {
	terms := parseQueryTerms(query)
	trigramSet := make(map[uint32]bool)
	for _, term := range terms {
		for _, t := range db.trigrams.ExtractTrigrams([]byte(term)) {
			trigramSet[t] = true
		}
	}
	result := make([]uint32, 0, len(trigramSet))
	for t := range trigramSet {
		result = append(result, t)
	}
	return result
}

// filterResults re-chunks files and keeps only results where matchFn returns true.
// Used by both literal verify and regex verify.
func filterResults(db *DB, results []SearchResult, matchFn func(chunkContent []byte) bool) []SearchResult {
	chunkCache := make(map[string]map[string][]byte)
	var verified []SearchResult
	for _, r := range results {
		chunks, ok := chunkCache[r.Path]
		if !ok {
			chunks = rechunkForVerify(db, r.Path)
			chunkCache[r.Path] = chunks
		}
		if chunks == nil {
			continue
		}

		chunkContent, ok := chunks[r.Range]
		if !ok {
			continue
		}
		if matchFn(chunkContent) {
			verified = append(verified, r)
		}
	}
	return verified
}

// rechunkForVerify looks up a file's strategy, re-chunks it, and returns
// a map from range string to chunk content.
func rechunkForVerify(db *DB, fpath string) map[string][]byte {
	var strategy string
	err := db.env.View(func(txn *lmdb.Txn) error {
		_, frec, err := db.lookupFileByPath(txnWrap{txn}, fpath)
		if err != nil {
			return err
		}
		strategy = frec.Strategy
		return nil
	})
	if err != nil {
		return nil
	}

	chunkFn := db.resolveChunkFunc(strategy)
	if chunkFn == nil {
		return nil
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil
	}

	chunks := make(map[string][]byte)
	chunkFn(fpath, data, func(c Chunk) bool {
		content := make([]byte, len(c.Content))
		copy(content, c.Content)
		chunks[string(c.Range)] = content
		return true
	})
	return chunks
}

// verifyResults re-chunks files and discards results where
// any query term is absent as a case-insensitive substring.
// R124
func verifyResults(db *DB, results []SearchResult, query string) []SearchResult {
	terms := parseQueryTerms(query)
	if len(terms) == 0 {
		return results
	}
	lowerTerms := make([][]byte, len(terms))
	for i, t := range terms {
		lowerTerms[i] = bytes.ToLower([]byte(t))
	}
	return filterResults(db, results, func(chunkContent []byte) bool {
		lowerChunk := bytes.ToLower(chunkContent)
		for _, term := range lowerTerms {
			if !bytes.Contains(lowerChunk, term) {
				return false
			}
		}
		return true
	})
}

// verifyResultsRegex re-chunks files and discards results
// where the compiled regex does not match.
func verifyResultsRegex(db *DB, results []SearchResult, re *regexp.Regexp) []SearchResult {
	return filterResults(db, results, func(chunkContent []byte) bool {
		return re.Match(chunkContent)
	})
}

// CRC: crc-DB.md | R279, R280, R281, R282
// proximityRerank reranks the top-N results by query term proximity in chunk text.
func proximityRerank(db *DB, results []SearchResult, query string, topN int) []SearchResult {
	if topN <= 0 || len(results) == 0 {
		return results
	}
	terms := parseQueryTerms(query)
	if len(terms) < 2 {
		return results // proximity only meaningful for multi-term queries
	}
	lowerTerms := make([]string, len(terms))
	for i, t := range terms {
		lowerTerms[i] = strings.ToLower(t)
	}

	if topN > len(results) {
		topN = len(results)
	}
	top := results[:topN]
	rest := results[topN:]

	for i := range top {
		content := rechunkContent(db, top[i].Path, top[i].Range)
		if content == nil {
			continue
		}
		span := minTermSpan(bytes.ToLower(content), lowerTerms)
		if span > 0 {
			top[i].Score += 1.0 / (1.0 + float64(span))
		}
	}

	sortResults(top)
	return append(top, rest...)
}

// rechunkContent recovers a single chunk's content by re-chunking the file.
func rechunkContent(db *DB, fpath, targetRange string) []byte {
	fn := db.resolveChunkFunc(fileStrategy(db, fpath))
	if fn == nil {
		return nil
	}
	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil
	}
	var result []byte
	fn(fpath, data, func(c Chunk) bool {
		if string(c.Range) == targetRange {
			result = make([]byte, len(c.Content))
			copy(result, c.Content)
			return false
		}
		return true
	})
	return result
}

// fileStrategy looks up the chunking strategy for a file.
func fileStrategy(db *DB, fpath string) string {
	var strategy string
	db.env.View(func(txn *lmdb.Txn) error {
		_, frec, err := db.lookupFileByPath(txnWrap{txn}, fpath)
		if err == nil {
			strategy = frec.Strategy
		}
		return nil
	})
	return strategy
}

// minTermSpan finds the minimum window (in words) containing all terms.
func minTermSpan(content []byte, terms []string) int {
	// Tokenize content into word positions
	words := bytes.Fields(content)
	if len(words) == 0 {
		return 0
	}

	// Find positions of each term
	termPositions := make([][]int, len(terms))
	allFound := true
	for ti, term := range terms {
		termBytes := []byte(term)
		for wi, word := range words {
			if bytes.Contains(word, termBytes) {
				termPositions[ti] = append(termPositions[ti], wi)
			}
		}
		if len(termPositions[ti]) == 0 {
			allFound = false
			break
		}
	}
	if !allFound {
		return 0
	}

	// Sliding window: find minimum span containing at least one position from each term
	best := len(words)
	// Use first term's positions as anchors
	for _, anchor := range termPositions[0] {
		lo, hi := anchor, anchor
		for ti := 1; ti < len(terms); ti++ {
			// Find closest position to the current window
			closest := termPositions[ti][0]
			for _, p := range termPositions[ti] {
				if abs(p-anchor) < abs(closest-anchor) {
					closest = p
				}
			}
			if closest < lo {
				lo = closest
			}
			if closest > hi {
				hi = closest
			}
		}
		span := hi - lo
		if span < best {
			best = span
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// applyRegexPostFilters compiles regex filter and except-regex patterns from
// the search config, then applies them as post-filters to the results.
// R183, R184, R186, R187, R188, R191
func applyRegexPostFilters(db *DB, results []SearchResult, cfg searchConfig) ([]SearchResult, error) {
	if len(cfg.regexFilters) == 0 && len(cfg.exceptRegexFilters) == 0 {
		return results, nil
	}
	andRegexes := make([]*regexp.Regexp, len(cfg.regexFilters))
	for i, p := range cfg.regexFilters {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compile filter-regex %q: %w", p, err)
		}
		andRegexes[i] = re
	}
	exceptRegexes := make([]*regexp.Regexp, len(cfg.exceptRegexFilters))
	for i, p := range cfg.exceptRegexFilters {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compile except-regex %q: %w", p, err)
		}
		exceptRegexes[i] = re
	}
	return filterResults(db, results, func(chunkContent []byte) bool {
		for _, re := range andRegexes {
			if !re.Match(chunkContent) {
				return false
			}
		}
		for _, re := range exceptRegexes {
			if re.Match(chunkContent) {
				return false
			}
		}
		return true
	}), nil
}


// --- SearchRegex ---

// Seq: seq-search.md
// SearchRegex searches using a regex pattern against the full trigram index.
func (db *DB) SearchRegex(pattern string, opts ...SearchOption) (*SearchResults, error) {
	cfg := applySearchOpts(opts)

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile regex: %w", err)
	}

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("parse regex: %w", err)
	}
	q := csindex.RegexpQuery(re)

	var results []SearchResult

	err = db.env.View(func(txn *lmdb.Txn) error {
		th := txnWrap{txn}
		candidates := evalTrigramQueryNew(q, th, db.dbi, db.trigrams)
		if candidates == nil {
			// QAll: match everything — collect all chunkids from all T records
			candidates = allChunkIDs(th, db.dbi)
		}

		cands := db.collectCandidates(th, candidates, cfg)
		results = db.scoreAndResolve(th, cands, nil, cfg.scoreFunc, cfg)
		return nil
	})
	if err != nil {
		return nil, err
	}

	results = verifyResultsRegex(db, results, compiled)

	// R188, R189, R190: apply regex post-filters after verify, before sort
	results, err = applyRegexPostFilters(db, results, cfg)
	if err != nil {
		return nil, err
	}

	if cfg.proximityTopN > 0 {
		results = proximityRerank(db, results, pattern, cfg.proximityTopN)
	}

	sortResults(results)
	return &SearchResults{
		Results: results,
		Status:  IndexStatus{Built: true},
	}, nil
}

// evalTrigramQueryNew recursively evaluates a codesearch trigram query using T records.
// Returns a set of candidate chunkids, or nil for QAll.
func evalTrigramQueryNew(q *csindex.Query, th TxnHolder, dbi lmdb.DBI, tg *Trigrams) map[uint64]bool {
	switch q.Op {
	case csindex.QAll:
		return nil
	case csindex.QNone:
		return make(map[uint64]bool)
	case csindex.QAnd:
		var result map[uint64]bool
		for _, tri := range q.Trigram {
			encoded, ok := tg.EncodeTrigram(tri)
			if !ok {
				continue
			}
			set := readTRecordChunkIDs(th, dbi, encoded)
			if result == nil {
				result = set
			} else {
				result = intersectChunkSets(result, set)
			}
		}
		for _, sub := range q.Sub {
			subSet := evalTrigramQueryNew(sub, th, dbi, tg)
			if subSet == nil {
				continue
			}
			if result == nil {
				result = subSet
			} else {
				result = intersectChunkSets(result, subSet)
			}
		}
		return result
	case csindex.QOr:
		result := make(map[uint64]bool)
		for _, tri := range q.Trigram {
			encoded, ok := tg.EncodeTrigram(tri)
			if !ok {
				continue
			}
			for id := range readTRecordChunkIDs(th, dbi, encoded) {
				result[id] = true
			}
		}
		for _, sub := range q.Sub {
			subSet := evalTrigramQueryNew(sub, th, dbi, tg)
			if subSet == nil {
				return nil // QAll in OR → everything
			}
			for id := range subSet {
				result[id] = true
			}
		}
		return result
	}
	return make(map[uint64]bool)
}

// readTRecordChunkIDs reads a T record and returns chunkid set.
func readTRecordChunkIDs(th TxnHolder, dbi lmdb.DBI, trigram uint32) map[uint64]bool {
	val, err := th.Txn().Get(dbi, makeTKey(trigram))
	if err != nil {
		return make(map[uint64]bool)
	}
	ids, _ := UnmarshalTValue(val)
	result := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result
}

// allChunkIDs scans all C records and returns every chunkid.
func allChunkIDs(th TxnHolder, dbi lmdb.DBI) map[uint64]bool {
	txn := th.Txn()
	result := make(map[uint64]bool)
	cursor, err := txn.OpenCursor(dbi)
	if err != nil {
		return result
	}
	defer cursor.Close()
	key, _, err := cursor.Get([]byte{prefixC}, nil, lmdb.SetRange)
	for err == nil && len(key) > 0 && key[0] == prefixC {
		chunkid, _ := readUvarint(key[1:])
		result[chunkid] = true
		key, _, err = cursor.Get(nil, nil, lmdb.Next)
	}
	return result
}


// --- FileInfoByID ---

// FileInfoByID resolves a fileid to its FRecord.
func (db *DB) FileInfoByID(fileid uint64) (FRecord, error) {
	var frec FRecord
	err := db.env.View(func(txn *lmdb.Txn) error {
		var err error
		frec, err = db.readFRecord(txnWrap{txn}, fileid)
		return err
	})
	return frec, err
}

// --- GetChunks ---

// Seq: seq-chunks.md | R197, R198, R199, R200, R201, R202, R203
// GetChunks retrieves the target chunk (identified by range label) and
// up to before/after positional neighbors. Returns chunks in positional order.
func (db *DB) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error) {
	var frec FRecord
	err := db.env.View(func(txn *lmdb.Txn) error {
		var err error
		_, frec, err = db.lookupFileByPath(txnWrap{txn}, fpath)
		return err
	})
	if err != nil {
		return nil, err
	}

	// Find target chunk index by exact range label match.
	targetIdx := -1
	for i, fce := range frec.Chunks {
		if fce.Location == targetRange {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return nil, fmt.Errorf("range %q not found in %s", targetRange, fpath)
	}

	// Compute window clamped to bounds.
	lo := max(0, targetIdx-before)
	hi := min(len(frec.Chunks)-1, targetIdx+after)

	// Re-chunk the file to recover content.
	chunkFn := db.resolveChunkFunc(frec.Strategy)
	if chunkFn == nil {
		return nil, fmt.Errorf("chunking strategy %q not registered", frec.Strategy)
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil, err
	}

	var results []ChunkResult
	idx := 0
	chunkFn(fpath, data, func(c Chunk) bool {
		if idx >= lo && idx <= hi {
			results = append(results, ChunkResult{
				Path:    fpath,
				Range:   string(c.Range),
				Content: string(c.Content),
				Index:   idx,
			})
		}
		idx++
		return idx <= hi // stop early once past window
	})

	return results, nil
}

// --- ScoreFile ---

// Seq: seq-score.md | R178, R179, R180
// ScoreFile returns per-chunk scores for a single file using the given scoring function.
func (db *DB) ScoreFile(query, fpath string, fn ScoreFunc, opts ...SearchOption) ([]ScoredChunk, error) {
	cfg := applySearchOpts(opts)
	query = strings.TrimSpace(query)
	queryTrigrams := extractPerTermTrigrams(db, query)
	if len(queryTrigrams) == 0 {
		return nil, nil
	}

	var results []ScoredChunk
	err := db.env.View(func(txn *lmdb.Txn) error {
		th := txnWrap{txn}
		_, frec, err := db.lookupFileByPath(th, fpath)
		if err != nil {
			return err
		}

		active := selectQueryTrigrams(th, db.dbi, queryTrigrams, cfg)
		if len(active) == 0 {
			return nil
		}

		// Read C records for each chunk and score
		for _, fce := range frec.Chunks {
			cVal, err := txn.Get(db.dbi, makeCKey(fce.ChunkID))
			if err != nil {
				// No C record — score as zero
				results = append(results, ScoredChunk{Range: fce.Location, Score: 0})
				continue
			}
			cData := make([]byte, len(cVal))
			copy(cData, cVal)
			crec, err := UnmarshalCValue(cData)
			if err != nil {
				results = append(results, ScoredChunk{Range: fce.Location, Score: 0})
				continue
			}

			chunkCounts := make(map[uint32]int, len(crec.Trigrams))
			for _, te := range crec.Trigrams {
				chunkCounts[te.Trigram] = te.Count
			}
			tokenCount := len(crec.Tokens)
			results = append(results, ScoredChunk{
				Range: fce.Location,
				Score: fn(active, chunkCounts, tokenCount),
			})
		}
		return nil
	})
	return results, err
}

// --- Built-in chunk functions ---

// LineChunkFunc is a built-in ChunkFunc that yields one chunk per line.
// Range is "N-N" (1-based line number).
func LineChunkFunc(_ string, content []byte, yield func(Chunk) bool) error {
	lineNum := 1
	start := 0
	for i, b := range content {
		if b == '\n' {
			r := fmt.Sprintf("%d-%d", lineNum, lineNum)
			if !yield(Chunk{Range: []byte(r), Content: content[start : i+1]}) {
				return nil
			}
			lineNum++
			start = i + 1
		}
	}
	if start < len(content) {
		r := fmt.Sprintf("%d-%d", lineNum, lineNum)
		yield(Chunk{Range: []byte(r), Content: content[start:]})
	}
	return nil
}

// --- Strategy management ---

func (db *DB) AddStrategy(name, cmd string) error {
	db.settings.ChunkingStrategies[name] = cmd
	return db.env.Update(func(txn *lmdb.Txn) error {
		return iPut(txnWrap{txn}, db.dbi, "strategy:"+name, cmd)
	})
}

// CRC: crc-DB.md | R116, R117
func (db *DB) AddStrategyFunc(name string, fn ChunkFunc) error {
	db.funcStrategies[name] = fn
	db.settings.ChunkingStrategies[name] = "" // empty cmd marks func strategy
	return db.env.Update(func(txn *lmdb.Txn) error {
		return iPut(txnWrap{txn}, db.dbi, "strategy:"+name, "")
	})
}

func (db *DB) RemoveStrategy(name string) error {
	delete(db.settings.ChunkingStrategies, name)
	delete(db.funcStrategies, name)
	return db.env.Update(func(txn *lmdb.Txn) error {
		return iDel(txnWrap{txn}, db.dbi, "strategy:"+name)
	})
}

// --- Staleness ---

// Seq: seq-stale.md

// CheckFile checks whether an indexed file is fresh, stale, or missing on disk.
func (db *DB) CheckFile(fpath string) (FileStatus, error) {
	var status FileStatus
	status.Path = fpath

	err := db.env.View(func(txn *lmdb.Txn) error {
		fileid, frec, err := db.lookupFileByPath(txnWrap{txn}, fpath)
		if err != nil {
			return err
		}
		status.FileID = fileid
		status.Strategy = frec.Strategy
		status.Status = classifyFile(frec)
		return nil
	})
	return status, err
}

// StaleFiles returns the status of every indexed file.
func (db *DB) StaleFiles() ([]FileStatus, error) {
	var frecords []FRecord

	err := db.env.View(func(txn *lmdb.Txn) error {
		cursor, err := txn.OpenCursor(db.dbi)
		if err != nil {
			return err
		}
		defer cursor.Close()

		key, val, err := cursor.Get([]byte{prefixF}, nil, lmdb.SetRange)
		for err == nil && len(key) > 0 && key[0] == prefixF {
			data := make([]byte, len(val))
			copy(data, val)
			frec, fErr := UnmarshalFValue(data)
			if fErr == nil {
				// Parse fileid from key
				fileid, _ := readUvarint(key[1:])
				frec.FileID = fileid
				frecords = append(frecords, frec)
			}
			key, val, err = cursor.Get(nil, nil, lmdb.Next)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var statuses []FileStatus
	for _, frec := range frecords {
		path := ""
		if len(frec.Names) > 0 {
			path = frec.Names[0]
		}
		statuses = append(statuses, FileStatus{
			Path:     path,
			FileID:   frec.FileID,
			Strategy: frec.Strategy,
			Status:   classifyFile(frec),
		})
	}
	return statuses, nil
}

// RefreshStale reindexes all stale files. If strategy is empty, each file's
// existing strategy is used. Returns the list of stale/missing files.
func (db *DB) RefreshStale(strategy string) ([]FileStatus, error) {
	statuses, err := db.StaleFiles()
	if err != nil {
		return nil, err
	}

	var result []FileStatus
	for _, fs := range statuses {
		switch fs.Status {
		case "stale":
			strat := strategy
			if strat == "" {
				strat = fs.Strategy
			}
			if _, err := db.Reindex(fs.Path, strat); err != nil {
				return result, fmt.Errorf("refresh %s: %w", fs.Path, err)
			}
			fs.Status = "refreshed"
			result = append(result, fs)
		case "missing":
			result = append(result, fs)
		}
	}
	return result, nil
}

// classifyFile determines whether a file is fresh, stale, or missing by
// comparing disk state to stored metadata. File I/O happens here, outside
// any LMDB transaction.
func classifyFile(frec FRecord) string {
	filename := ""
	if len(frec.Names) > 0 {
		filename = frec.Names[0]
	}
	fi, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return "missing"
	}
	if err != nil {
		return "missing"
	}
	if fi.ModTime().UnixNano() == frec.ModTime {
		return "fresh"
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return "missing"
	}
	h := sha256.Sum256(data)
	if h == frec.ContentHash {
		return "fresh"
	}
	return "stale"
}

// --- Helpers ---


// sortResults sorts search results by filename then start line.
// Seq: seq-search.md | R33
func sortResults(results []SearchResult) {
	slices.SortFunc(results, func(a, b SearchResult) int {
		// Sort by score descending (higher scores first)
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		// Tie-break by path then start line
		if c := cmp.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return cmp.Compare(a.Range, b.Range)
	})
}

// fileModTime returns the file's modification time. Call this before reading
// file data so the recorded mod time precedes the read — if the file changes
// between stat and read, it will appear stale on next check (safe direction).
func fileModTime(fpath string) (int64, error) {
	fi, err := os.Stat(fpath)
	if err != nil {
		return 0, err
	}
	return fi.ModTime().UnixNano(), nil
}

func contentHash(data []byte) [32]byte {
	return sha256.Sum256(data)
}


// allocFileID reads the next file ID counter and increments it atomically.
func (db *DB) allocFileID(th TxnHolder) (uint64, error) {
	id, err := iCounter(th, db.dbi, "nextFileID")
	if err != nil {
		return 0, err
	}
	if id == 0 {
		id = 1 // should not happen after Create, but be safe
	}
	if err := iSetCounter(th, db.dbi, "nextFileID", id+1); err != nil {
		return 0, err
	}
	return id, nil
}

// allocChunkID reads the next chunk ID counter and increments it atomically.
func (db *DB) allocChunkID(th TxnHolder) (uint64, error) {
	id, err := iCounter(th, db.dbi, "nextChunkID")
	if err != nil {
		return 0, err
	}
	if id == 0 {
		id = 1
	}
	if err := iSetCounter(th, db.dbi, "nextChunkID", id+1); err != nil {
		return 0, err
	}
	return id, nil
}

func (db *DB) saveSettings() error {
	return db.env.Update(func(txn *lmdb.Txn) error {
		return writeSettings(txnWrap{txn}, db.dbi, &db.settings)
	})
}

