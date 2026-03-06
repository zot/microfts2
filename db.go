package microfts2

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"regexp/syntax"

	csindex "github.com/google/codesearch/index"

	"github.com/bmatsuo/lmdb-go/lmdb"
)

// CRC: crc-DB.md | Seq: seq-init.md, seq-add.md, seq-search.md, seq-score.md, seq-build-index.md, seq-stale.md

// chunkID identifies a specific chunk within a file.
type chunkID struct{ fileid, chunknum uint64 }

// Record prefixes for content DB keys.
const (
	prefixA = 'A' // active trigrams (sparse packed sorted list)
	prefixC = 'C' // trigram counts (sparse: C[tri:3] -> count:8)
	prefixI = 'I' // settings JSON
	prefixN = 'N' // file info JSON
)

// prefixR is the reverse index prefix in the index DB.
const prefixR = 'R'

// DB is the main database handle.
// ChunkFunc is a Go function that computes chunk boundaries for a file.
// It receives the file path and content, and returns byte offsets.
type ChunkFunc func(path string, content []byte) ([]int64, error)

type DB struct {
	env            *lmdb.Env
	contentDBI     lmdb.DBI
	indexDBI        lmdb.DBI
	activeTrigrams []uint32 // sorted, loaded from A record
	settings       Settings
	trigrams       *Trigrams
	contentName    string
	indexName      string
	funcStrategies map[string]ChunkFunc // in-memory Go function strategies
}

// Settings is stored as JSON in the I record.
type Settings struct {
	CaseInsensitive    bool              `json:"caseInsensitive"`
	Aliases            map[string]string `json:"aliases,omitempty"`
	ChunkingStrategies map[string]string `json:"chunkingStrategies"`
	SearchCutoff       int               `json:"searchCutoff"`
	NextFileID         uint64            `json:"nextFileID"`
}

// SearchResult is a single match from Search.
type SearchResult struct {
	Path      string
	StartLine int
	EndLine   int
	Score     float64
}

// ScoredChunk is a per-chunk trigram match score from ScoreFile.
type ScoredChunk struct {
	StartLine int
	EndLine   int
	Score     float64
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

// FileInfo is stored as JSON in N records.
type FileInfo struct {
	Filename         string  `json:"filename"`
	ChunkOffsets     []int64 `json:"chunkOffsets"`
	ChunkStartLines  []int   `json:"chunkStartLines"`
	ChunkEndLines    []int   `json:"chunkEndLines"`
	ChunkTokenCounts []int   `json:"chunkTokenCounts"`
	ChunkingStrategy string  `json:"chunkingStrategy"`
	ModTime          int64   `json:"modTime"`
	ContentHash      string  `json:"contentHash"`
}

// ScoreFunc computes a relevance score for a chunk.
// queryTrigrams: active query trigrams.
// chunkCounts: trigram -> occurrence count in the chunk.
// chunkTokenCount: number of tokens (words) in the chunk.
type ScoreFunc func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64

// SearchOption configures search behavior.
type SearchOption func(*searchConfig)

type searchConfig struct {
	scoreFunc ScoreFunc
	onlyIDs   map[uint64]struct{} // if non-nil, only include these file IDs
	exceptIDs map[uint64]struct{} // if non-nil, exclude these file IDs
	verify    bool                // post-filter: verify query terms in chunk text
}

// WithCoverage uses coverage scoring (default): matching / total active query trigrams.
func WithCoverage() SearchOption {
	return func(c *searchConfig) { c.scoreFunc = scoreCoverage }
}

// WithDensity uses token-density scoring for long queries.
func WithDensity() SearchOption {
	return func(c *searchConfig) { c.scoreFunc = scoreDensity }
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
	Aliases         map[byte]byte
	ContentDBName   string
	IndexDBName     string
	MaxDBs          int   // LMDB max named databases, default 2
	MapSize         int64 // bytes, default 1GB
}

func (o *Options) contentDB() string {
	if o.ContentDBName != "" {
		return o.ContentDBName
	}
	return "ftscontent"
}

func (o *Options) indexDB() string {
	if o.IndexDBName != "" {
		return o.IndexDBName
	}
	return "ftsindex"
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

func makeNKey(fileid uint64) []byte {
	key := make([]byte, 9)
	key[0] = prefixN
	binary.BigEndian.PutUint64(key[1:], fileid)
	return key
}

// makeCKey builds a C record key: C[trigram:3] = 4 bytes.
func makeCKey(trigram uint32) []byte {
	key := make([]byte, 4)
	key[0] = prefixC
	key[1] = byte(trigram >> 16)
	key[2] = byte(trigram >> 8)
	key[3] = byte(trigram)
	return key
}

// makeIndexKey builds a forward index key: [trigram:3][descCount:2][fileid:8][chunknum:8] = 21 bytes.
func makeIndexKey(trigram uint32, count uint16, fileid, chunknum uint64) []byte {
	key := make([]byte, 21)
	key[0] = byte(trigram >> 16)
	key[1] = byte(trigram >> 8)
	key[2] = byte(trigram)
	binary.BigEndian.PutUint16(key[3:], 0xFFFF-count) // descending
	binary.BigEndian.PutUint64(key[5:], fileid)
	binary.BigEndian.PutUint64(key[13:], chunknum)
	return key
}

func indexKeyTrigram(key []byte) uint32 {
	return uint32(key[0])<<16 | uint32(key[1])<<8 | uint32(key[2])
}

func indexKeyCount(key []byte) uint16 {
	return 0xFFFF - binary.BigEndian.Uint16(key[3:])
}

func indexKeyFileID(key []byte) uint64 {
	return binary.BigEndian.Uint64(key[5:])
}

func indexKeyChunkNum(key []byte) uint64 {
	return binary.BigEndian.Uint64(key[13:])
}

// makeRKey builds a reverse index key: R[fileid:8] = 9 bytes.
func makeRKey(fileid uint64) []byte {
	key := make([]byte, 9)
	key[0] = prefixR
	binary.BigEndian.PutUint64(key[1:], fileid)
	return key
}

// chunkTrigramEntry is one (trigram, count) pair in an R record.
type chunkTrigramEntry struct {
	trigram uint32
	count   uint16
}

// encodeRValue packs chunk-grouped trigram data for an R record.
// Input: map of chunknum -> slice of (trigram, count) pairs.
func encodeRValue(chunks map[uint64][]chunkTrigramEntry) []byte {
	// Calculate total size
	size := 0
	for _, entries := range chunks {
		size += 8 + 2 + len(entries)*5 // chunknum(8) + numTrigrams(2) + entries * (trigram(3) + count(2))
	}
	buf := make([]byte, size)
	off := 0
	for chunknum, entries := range chunks {
		binary.BigEndian.PutUint64(buf[off:], chunknum)
		off += 8
		binary.BigEndian.PutUint16(buf[off:], uint16(len(entries)))
		off += 2
		for _, e := range entries {
			buf[off] = byte(e.trigram >> 16)
			buf[off+1] = byte(e.trigram >> 8)
			buf[off+2] = byte(e.trigram)
			binary.BigEndian.PutUint16(buf[off+3:], e.count)
			off += 5
		}
	}
	return buf
}

// decodeRValue unpacks an R record value into (chunknum, trigram, count) triples.
func decodeRValue(data []byte) []struct {
	chunknum uint64
	trigram  uint32
	count    uint16
} {
	var result []struct {
		chunknum uint64
		trigram  uint32
		count    uint16
	}
	off := 0
	for off+10 <= len(data) { // need at least chunknum(8) + numTrigrams(2)
		chunknum := binary.BigEndian.Uint64(data[off:])
		off += 8
		n := int(binary.BigEndian.Uint16(data[off:]))
		off += 2
		for i := 0; i < n && off+5 <= len(data); i++ {
			tri := uint32(data[off])<<16 | uint32(data[off+1])<<8 | uint32(data[off+2])
			count := binary.BigEndian.Uint16(data[off+3:])
			result = append(result, struct {
				chunknum uint64
				trigram  uint32
				count    uint16
			}{chunknum, tri, count})
			off += 5
		}
	}
	return result
}

// filterActiveQueryTrigrams returns deduplicated query trigrams present in the active set.
func filterActiveQueryTrigrams(activeSet map[uint32]bool, queryTrigrams []uint32) []uint32 {
	seen := make(map[uint32]bool)
	var active []uint32
	for _, t := range queryTrigrams {
		if activeSet[t] && !seen[t] {
			seen[t] = true
			active = append(active, t)
		}
	}
	return active
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
		Aliases:            byteAliasesToJSON(opts.Aliases),
		ChunkingStrategies: make(map[string]string),
		SearchCutoff:       50,
		NextFileID:         1,
	}

	db := &DB{
		env:            env,
		trigrams:       NewTrigrams(opts.CaseInsensitive, opts.Aliases),
		contentName:    opts.contentDB(),
		indexName:      opts.indexDB(),
		settings:       settings,
		funcStrategies: make(map[string]ChunkFunc),
	}

	err = env.Update(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI(db.contentName, lmdb.Create)
		if err != nil {
			return err
		}
		db.contentDBI = dbi

		idbi, err := txn.OpenDBI(db.indexName, lmdb.Create)
		if err != nil {
			return err
		}
		db.indexDBI = idbi

		settingsJSON, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		return txn.Put(dbi, []byte{prefixI}, settingsJSON, 0)
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

	db := &DB{
		env:            env,
		contentName:    opts.contentDB(),
		indexName:      opts.indexDB(),
		funcStrategies: make(map[string]ChunkFunc),
	}

	// Open content DB and load settings in a write txn (ensures DBI handle is committed)
	err = env.Update(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI(db.contentName, 0)
		if err != nil {
			return fmt.Errorf("open content db %q: %w", db.contentName, err)
		}
		db.contentDBI = dbi

		val, err := txn.Get(dbi, []byte{prefixI})
		if err != nil {
			return fmt.Errorf("read settings: %w", err)
		}
		data := make([]byte, len(val))
		copy(data, val)
		if err := json.Unmarshal(data, &db.settings); err != nil {
			return fmt.Errorf("parse settings: %w", err)
		}

		// Load A record (packed sorted trigram list) if present
		aVal, aErr := txn.Get(dbi, []byte{prefixA})
		if aErr == nil && len(aVal)%3 == 0 {
			db.activeTrigrams = decodePackedTrigrams(aVal)
		}

		// Open index DB (always exists — maintained incrementally)
		idbi, err := txn.OpenDBI(db.indexName, lmdb.Create)
		if err != nil {
			return fmt.Errorf("open index db %q: %w", db.indexName, err)
		}
		db.indexDBI = idbi
		return nil
	})
	if err != nil {
		env.Close()
		return nil, err
	}

	db.trigrams = NewTrigrams(db.settings.CaseInsensitive, byteAliasesFromJSON(db.settings.Aliases))

	return db, nil
}

// Settings returns the current database settings.
func (db *DB) Settings() Settings {
	return db.settings
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

// Seq: seq-add.md | R118
func (db *DB) addFileCore(fpath, strategy string) (uint64, []byte, error) {
	if _, ok := db.settings.ChunkingStrategies[strategy]; !ok {
		return 0, nil, fmt.Errorf("unknown chunking strategy: %s", strategy)
	}

	modTime, err := fileModTime(fpath)
	if err != nil {
		return 0, nil, err
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		return 0, nil, err
	}

	if !utf8.Valid(data) {
		return 0, nil, fmt.Errorf("file contains invalid UTF-8: %s", fpath)
	}

	offsets, err := db.runStrategy(strategy, fpath, data)
	if err != nil {
		return 0, nil, err
	}

	hash := contentHash(data)
	boundaries := normalizeBoundaries(offsets, int64(len(data)))
	startLines, endLines := computeChunkLines(data, boundaries)

	var fileid uint64
	err = db.env.Update(func(txn *lmdb.Txn) error {
		var txnErr error
		fileid, txnErr = db.addFileInTxn(txn, fpath, strategy, data, boundaries, startLines, endLines, modTime, hash)
		return txnErr
	})
	return fileid, data, err
}

// runStrategy dispatches to func strategy or external chunker.
func (db *DB) runStrategy(strategy, fpath string, data []byte) ([]int64, error) {
	if fn, ok := db.funcStrategies[strategy]; ok {
		return fn(fpath, data)
	}
	cmd := db.settings.ChunkingStrategies[strategy]
	if cmd == "" {
		return nil, fmt.Errorf("func strategy %q not registered (re-register with AddStrategyFunc after Open)", strategy)
	}
	return RunChunker(cmd, fpath)
}

func (db *DB) addFileInTxn(txn *lmdb.Txn, fpath, strategy string, data []byte, boundaries []int64, startLines, endLines []int, modTime int64, hash string) (uint64, error) {
	fileid, err := db.allocFileID(txn)
	if err != nil {
		return 0, err
	}

	// Write F records
	pairs := EncodeFilename(fpath)
	fileidBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(fileidBytes, fileid)
	for i, pair := range pairs {
		val := []byte{}
		if i == len(pairs)-1 {
			val = fileidBytes
		}
		if err := txn.Put(db.contentDBI, pair.Key, val, 0); err != nil {
			return 0, err
		}
	}

	// Process each chunk — compute trigram counts, write forward index entries
	rChunks := make(map[uint64][]chunkTrigramEntry)
	tokenCounts := make([]int, len(boundaries)-1)

	for i := 0; i < len(boundaries)-1; i++ {
		start := boundaries[i]
		end := boundaries[i+1]
		chunkData := data[start:end]
		chunknum := uint64(i)

		triCounts := db.trigrams.TrigramCounts(chunkData)
		tokenCounts[i] = countTokens(chunkData)

		var entries []chunkTrigramEntry
		for tri, cnt := range triCounts {
			// Update sparse C record
			if err := incrementCCount(txn, db.contentDBI, tri); err != nil {
				return 0, err
			}

			// Clamp count for index key
			idxCount := uint16(cnt)
			if cnt > 65535 {
				idxCount = 65535
			}

			// Write forward index entry
			if err := txn.Put(db.indexDBI, makeIndexKey(tri, idxCount, fileid, chunknum), []byte{}, 0); err != nil {
				return 0, err
			}

			entries = append(entries, chunkTrigramEntry{tri, idxCount})
		}
		rChunks[chunknum] = entries
	}

	// Write R record (reverse index)
	if err := txn.Put(db.indexDBI, makeRKey(fileid), encodeRValue(rChunks), 0); err != nil {
		return 0, err
	}

	// Write N record
	info := FileInfo{
		Filename:         fpath,
		ChunkOffsets:     boundaries[:len(boundaries)-1],
		ChunkStartLines:  startLines,
		ChunkEndLines:    endLines,
		ChunkTokenCounts: tokenCounts,
		ChunkingStrategy: strategy,
		ModTime:          modTime,
		ContentHash:      hash,
	}
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return 0, err
	}
	return fileid, txn.Put(db.contentDBI, makeNKey(fileid), infoJSON, 0)
}

// --- RemoveFile ---

func (db *DB) RemoveFile(fpath string) error {
	return db.env.Update(func(txn *lmdb.Txn) error {
		return db.removeFileInTxn(txn, fpath)
	})
}

func (db *DB) removeFileInTxn(txn *lmdb.Txn, fpath string) error {
	finalKey := FinalKey(fpath)
	val, err := txn.Get(db.contentDBI, finalKey)
	if lmdb.IsNotFound(err) {
		return fmt.Errorf("file not found: %s", fpath)
	} else if err != nil {
		return fmt.Errorf("lookup %s: %w", fpath, err)
	}
	fileid := binary.BigEndian.Uint64(val)

	// Read R record to find all forward index entries
	rKey := makeRKey(fileid)
	rVal, err := txn.Get(db.indexDBI, rKey)
	if err != nil && !lmdb.IsNotFound(err) {
		return fmt.Errorf("read R record for %s: %w", fpath, err)
	}

	if rVal != nil {
		entries := decodeRValue(slices.Clone(rVal))
		for _, e := range entries {
			// Delete forward index entry
			txn.Del(db.indexDBI, makeIndexKey(e.trigram, e.count, fileid, e.chunknum), nil)

			// Decrement sparse C record
			decrementCCount(txn, db.contentDBI, e.trigram)
		}

		// Delete R record
		txn.Del(db.indexDBI, rKey, nil)
	}

	if err := txn.Del(db.contentDBI, makeNKey(fileid), nil); err != nil {
		return err
	}

	for _, pair := range EncodeFilename(fpath) {
		txn.Del(db.contentDBI, pair.Key, nil)
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
	if _, ok := db.settings.ChunkingStrategies[strategy]; !ok {
		return 0, nil, fmt.Errorf("unknown chunking strategy: %s", strategy)
	}

	modTime, err := fileModTime(fpath)
	if err != nil {
		return 0, nil, err
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		return 0, nil, err
	}

	offsets, err := db.runStrategy(strategy, fpath, data)
	if err != nil {
		return 0, nil, err
	}

	hash := contentHash(data)
	boundaries := normalizeBoundaries(offsets, int64(len(data)))
	startLines, endLines := computeChunkLines(data, boundaries)

	var fileid uint64
	err = db.env.Update(func(txn *lmdb.Txn) error {
		if err := db.removeFileInTxn(txn, fpath); err != nil {
			return err
		}
		var txnErr error
		fileid, txnErr = db.addFileInTxn(txn, fpath, strategy, data, boundaries, startLines, endLines, modTime, hash)
		return txnErr
	})
	return fileid, data, err
}

// --- Search ---

// chunkCandidate tracks accumulated trigram counts for a candidate chunk.
type chunkCandidate struct {
	counts map[uint32]int
}

// Seq: seq-search.md
func (db *DB) Search(query string, opts ...SearchOption) (*SearchResults, error) {
	cfg := applySearchOpts(opts)

	queryTrigrams := db.trigrams.ExtractTrigrams([]byte(query))
	if len(queryTrigrams) == 0 {
		return &SearchResults{Status: IndexStatus{Built: true}}, nil
	}

	var results []SearchResult

	err := db.env.View(func(txn *lmdb.Txn) error {
		// Compute active set from sparse C records in the same snapshot as index scan
		activeSet := computeActiveSet(txn, db.contentDBI, db.settings.SearchCutoff)
		active := filterActiveQueryTrigrams(activeSet, queryTrigrams)
		if len(active) == 0 {
			return nil
		}

		cursor, err := txn.OpenCursor(db.indexDBI)
		if err != nil {
			return err
		}
		defer cursor.Close()

		// Collect candidates from first trigram, accumulating counts
		candidates := make(map[chunkID]*chunkCandidate)
		scanTrigram(cursor, active[0], func(fid, cnum uint64, count uint16) {
			id := chunkID{fid, cnum}
			cc := &chunkCandidate{counts: make(map[uint32]int)}
			cc.counts[active[0]] = int(count)
			candidates[id] = cc
		})

		// Intersect with remaining trigrams
		for _, tri := range active[1:] {
			next := make(map[chunkID]*chunkCandidate)
			scanTrigram(cursor, tri, func(fid, cnum uint64, count uint16) {
				id := chunkID{fid, cnum}
				if cc, ok := candidates[id]; ok {
					cc.counts[tri] = int(count)
					next[id] = cc
				}
			})
			candidates = next
		}

		results = resolveResults(txn, db.contentDBI, candidates, active, cfg)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if cfg.verify {
		results = verifyResults(results, query)
	}

	sortResults(results)
	return &SearchResults{
		Results: results,
		Status:  IndexStatus{Built: true},
	}, nil
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

// verifyResults reads chunk text from disk and discards results where
// any query term is absent as a case-insensitive substring.
// R124
func verifyResults(results []SearchResult, query string) []SearchResult {
	terms := parseQueryTerms(query)
	if len(terms) == 0 {
		return results
	}
	// Lowercase all terms for case-insensitive comparison
	lowerTerms := make([][]byte, len(terms))
	for i, t := range terms {
		lowerTerms[i] = bytes.ToLower([]byte(t))
	}

	// Cache file contents by path
	fileCache := make(map[string][]byte)
	var verified []SearchResult
	for _, r := range results {
		content, ok := fileCache[r.Path]
		if !ok {
			data, err := os.ReadFile(r.Path)
			if err != nil {
				continue // file unreadable, discard
			}
			content = data
			fileCache[r.Path] = content
		}

		// Extract chunk text by line range
		chunk := extractChunkByLines(content, r.StartLine, r.EndLine)
		lowerChunk := bytes.ToLower(chunk)

		match := true
		for _, term := range lowerTerms {
			if !bytes.Contains(lowerChunk, term) {
				match = false
				break
			}
		}
		if match {
			verified = append(verified, r)
		}
	}
	return verified
}

// extractChunkByLines returns the bytes spanning lines start..end (1-based inclusive).
func extractChunkByLines(content []byte, startLine, endLine int) []byte {
	line := 1
	chunkStart := -1
	for i, b := range content {
		if line == startLine && chunkStart < 0 {
			chunkStart = i
		}
		if b == '\n' {
			if line == endLine {
				return content[chunkStart : i+1]
			}
			line++
		}
	}
	// Handle last line without trailing newline
	if chunkStart >= 0 {
		return content[chunkStart:]
	}
	return nil
}

// verifyResultsRegex reads chunk text from disk and discards results
// where the compiled regex does not match.
func verifyResultsRegex(results []SearchResult, re *regexp.Regexp) []SearchResult {
	fileCache := make(map[string][]byte)
	var verified []SearchResult
	for _, r := range results {
		content, ok := fileCache[r.Path]
		if !ok {
			data, err := os.ReadFile(r.Path)
			if err != nil {
				continue
			}
			content = data
			fileCache[r.Path] = content
		}
		chunk := extractChunkByLines(content, r.StartLine, r.EndLine)
		if re.Match(chunk) {
			verified = append(verified, r)
		}
	}
	return verified
}

// resolveResults maps candidates to SearchResults, scoring each chunk.
func resolveResults(txn *lmdb.Txn, dbi lmdb.DBI, candidates map[chunkID]*chunkCandidate, active []uint32, cfg searchConfig) []SearchResult {
	infoCache := make(map[uint64]*FileInfo)
	var results []SearchResult
	for id, cc := range candidates {
		if cfg.onlyIDs != nil {
			if _, ok := cfg.onlyIDs[id.fileid]; !ok {
				continue
			}
		}
		if cfg.exceptIDs != nil {
			if _, ok := cfg.exceptIDs[id.fileid]; ok {
				continue
			}
		}
		info, ok := infoCache[id.fileid]
		if !ok {
			fi, err := readFileInfo(txn, dbi, id.fileid)
			if err != nil {
				continue
			}
			info = &fi
			infoCache[id.fileid] = info
		}
		idx := int(id.chunknum)
		if idx < len(info.ChunkStartLines) && idx < len(info.ChunkEndLines) {
			tokenCount := 0
			if idx < len(info.ChunkTokenCounts) {
				tokenCount = info.ChunkTokenCounts[idx]
			}
			score := cfg.scoreFunc(active, cc.counts, tokenCount)
			results = append(results, SearchResult{
				Path:      info.Filename,
				StartLine: info.ChunkStartLines[idx],
				EndLine:   info.ChunkEndLines[idx],
				Score:     score,
			})
		}
	}
	return results
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
		cursor, err := txn.OpenCursor(db.indexDBI)
		if err != nil {
			return err
		}
		defer cursor.Close()

		candidates := evalTrigramQuery(q, cursor, db.trigrams)
		if candidates == nil {
			// QAll: match everything — scan all N records for chunk IDs
			candidates = allChunks(txn, db.contentDBI)
		}

		results = resolveResults(txn, db.contentDBI, candidates, nil, cfg)
		return nil
	})
	if err != nil {
		return nil, err
	}

	results = verifyResultsRegex(results, compiled)
	sortResults(results)
	return &SearchResults{
		Results: results,
		Status:  IndexStatus{Built: true},
	}, nil
}

// evalTrigramQuery recursively evaluates a codesearch trigram query against the index.
func evalTrigramQuery(q *csindex.Query, cursor *lmdb.Cursor, tg *Trigrams) map[chunkID]*chunkCandidate {
	switch q.Op {
	case csindex.QAll:
		return nil
	case csindex.QNone:
		return make(map[chunkID]*chunkCandidate)
	case csindex.QAnd:
		var result map[chunkID]*chunkCandidate
		for _, tri := range q.Trigram {
			encoded, ok := tg.EncodeTrigram(tri)
			if !ok {
				continue
			}
			set := make(map[chunkID]*chunkCandidate)
			scanTrigram(cursor, encoded, func(fid, cnum uint64, count uint16) {
				id := chunkID{fid, cnum}
				cc := &chunkCandidate{counts: make(map[uint32]int)}
				cc.counts[encoded] = int(count)
				set[id] = cc
			})
			if result == nil {
				result = set
			} else {
				result = intersectCandidates(result, set)
			}
		}
		for _, sub := range q.Sub {
			subSet := evalTrigramQuery(sub, cursor, tg)
			if subSet == nil {
				continue
			}
			if result == nil {
				result = subSet
			} else {
				result = intersectCandidates(result, subSet)
			}
		}
		return result
	case csindex.QOr:
		result := make(map[chunkID]*chunkCandidate)
		for _, tri := range q.Trigram {
			encoded, ok := tg.EncodeTrigram(tri)
			if !ok {
				continue
			}
			scanTrigram(cursor, encoded, func(fid, cnum uint64, count uint16) {
				id := chunkID{fid, cnum}
				cc, ok := result[id]
				if !ok {
					cc = &chunkCandidate{counts: make(map[uint32]int)}
					result[id] = cc
				}
				cc.counts[encoded] = int(count)
			})
		}
		for _, sub := range q.Sub {
			subSet := evalTrigramQuery(sub, cursor, tg)
			if subSet == nil {
				return nil
			}
			for id, cc := range subSet {
				if existing, ok := result[id]; ok {
					for tri, cnt := range cc.counts {
						existing.counts[tri] = cnt
					}
				} else {
					result[id] = cc
				}
			}
		}
		return result
	}
	return make(map[chunkID]*chunkCandidate)
}

// intersectCandidates returns elements present in both sets, merging counts.
func intersectCandidates(a, b map[chunkID]*chunkCandidate) map[chunkID]*chunkCandidate {
	if len(a) > len(b) {
		a, b = b, a
	}
	result := make(map[chunkID]*chunkCandidate)
	for id, ccA := range a {
		if ccB, ok := b[id]; ok {
			for tri, cnt := range ccB.counts {
				ccA.counts[tri] = cnt
			}
			result[id] = ccA
		}
	}
	return result
}

// allChunks scans all N records and returns every chunkID in the content DB.
func allChunks(txn *lmdb.Txn, dbi lmdb.DBI) map[chunkID]*chunkCandidate {
	result := make(map[chunkID]*chunkCandidate)
	cursor, err := txn.OpenCursor(dbi)
	if err != nil {
		return result
	}
	defer cursor.Close()
	key, val, err := cursor.Get([]byte{prefixN}, nil, lmdb.SetRange)
	for err == nil && len(key) > 0 && key[0] == prefixN {
		if len(key) == 9 {
			fid := binary.BigEndian.Uint64(key[1:9])
			data := make([]byte, len(val))
			copy(data, val)
			var info FileInfo
			if json.Unmarshal(data, &info) == nil {
				for i := range info.ChunkOffsets {
					result[chunkID{fid, uint64(i)}] = &chunkCandidate{counts: make(map[uint32]int)}
				}
			}
		}
		key, val, err = cursor.Get(nil, nil, lmdb.Next)
	}
	return result
}

func scanTrigram(cursor *lmdb.Cursor, trigram uint32, fn func(fileid, chunknum uint64, count uint16)) {
	startKey := makeIndexKey(trigram, 65535, 0, 0) // max count = min descending value
	endTri := trigram + 1
	key, _, err := cursor.Get(startKey, nil, lmdb.SetRange)
	for err == nil && len(key) >= 21 {
		if indexKeyTrigram(key) >= endTri {
			break
		}
		fn(indexKeyFileID(key), indexKeyChunkNum(key), indexKeyCount(key))
		key, _, err = cursor.Get(nil, nil, lmdb.Next)
	}
}

// --- FileInfoByID ---

// FileInfoByID resolves a fileid to its FileInfo (N record).
func (db *DB) FileInfoByID(fileid uint64) (FileInfo, error) {
	var info FileInfo
	err := db.env.View(func(txn *lmdb.Txn) error {
		var err error
		info, err = readFileInfo(txn, db.contentDBI, fileid)
		return err
	})
	return info, err
}

// --- ScoreFile ---

// Seq: seq-score.md
// ScoreFile returns per-chunk scores for a single file using the given scoring function.
func (db *DB) ScoreFile(query, fpath string, fn ScoreFunc) ([]ScoredChunk, error) {
	queryTrigrams := db.trigrams.ExtractTrigrams([]byte(query))
	if len(queryTrigrams) == 0 {
		return nil, nil
	}

	var results []ScoredChunk
	err := db.env.View(func(txn *lmdb.Txn) error {
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.contentDBI, finalKey)
		if err != nil {
			return fmt.Errorf("file not found: %s", fpath)
		}
		fileid := binary.BigEndian.Uint64(val)

		info, err := readFileInfo(txn, db.contentDBI, fileid)
		if err != nil {
			return err
		}

		// Compute active query trigrams from sparse C records
		activeSet := computeActiveSet(txn, db.contentDBI, db.settings.SearchCutoff)
		active := filterActiveQueryTrigrams(activeSet, queryTrigrams)
		if len(active) == 0 {
			return nil
		}

		// Read R record to get per-chunk trigram counts
		rKey := makeRKey(fileid)
		rVal, err := txn.Get(db.indexDBI, rKey)
		if err != nil {
			return nil // no R record = no scores
		}
		entries := decodeRValue(slices.Clone(rVal))

		// Group by chunk
		chunkCounts := make(map[uint64]map[uint32]int)
		for _, e := range entries {
			m, ok := chunkCounts[e.chunknum]
			if !ok {
				m = make(map[uint32]int)
				chunkCounts[e.chunknum] = m
			}
			m[e.trigram] = int(e.count)
		}

		for i := range info.ChunkOffsets {
			counts := chunkCounts[uint64(i)]
			if counts == nil {
				counts = make(map[uint32]int)
			}
			tokenCount := 0
			if i < len(info.ChunkTokenCounts) {
				tokenCount = info.ChunkTokenCounts[i]
			}
			results = append(results, ScoredChunk{
				StartLine: info.ChunkStartLines[i],
				EndLine:   info.ChunkEndLines[i],
				Score:     fn(active, counts, tokenCount),
			})
		}
		return nil
	})
	return results, err
}

// --- BuildIndex ---

// Seq: seq-build-index.md
// BuildIndex recomputes the A record (active trigram set) from C counts.
// Index entries are maintained incrementally by AddFile/RemoveFile.
func (db *DB) BuildIndex(cutoff int) error {
	return db.env.Update(func(txn *lmdb.Txn) error {
		// Scan all sparse C records, sort by count, take bottom cutoff%
		activeTrigrams := computeActiveSlice(txn, db.contentDBI, cutoff)

		if err := txn.Put(db.contentDBI, []byte{prefixA}, encodePackedTrigrams(activeTrigrams), 0); err != nil {
			return err
		}

		db.settings.SearchCutoff = cutoff
		if err := putSettings(txn, db.contentDBI, &db.settings); err != nil {
			return err
		}

		db.activeTrigrams = activeTrigrams
		return nil
	})
}

// --- Trigram inspection ---

// TrigramInfo describes a single trigram from a query.
type TrigramInfo struct {
	Trigram string  `json:"trigram"`
	Code    uint32  `json:"code"`
	Active  bool    `json:"active"`
	DocFreq int     `json:"docFreq"`
}

// QueryTrigrams extracts trigrams from a query string and reports which are
// active at the current cutoff and their document frequency (C record count).
func (db *DB) QueryTrigrams(query string) ([]TrigramInfo, error) {
	rawTrigrams := db.trigrams.ExtractTrigrams([]byte(query))
	if len(rawTrigrams) == 0 {
		return nil, nil
	}

	// Deduplicate while preserving order
	seen := make(map[uint32]bool)
	var unique []uint32
	for _, tri := range rawTrigrams {
		if !seen[tri] {
			seen[tri] = true
			unique = append(unique, tri)
		}
	}

	// Build active set for O(1) lookup
	activeSet := make(map[uint32]bool, len(db.activeTrigrams))
	for _, t := range db.activeTrigrams {
		activeSet[t] = true
	}

	var results []TrigramInfo
	err := db.env.View(func(txn *lmdb.Txn) error {
		for _, tri := range unique {
			info := TrigramInfo{
				Trigram: DecodeTrigram(tri),
				Code:    tri,
				Active:  activeSet[tri],
			}
			cKey := makeCKey(tri)
			if val, err := txn.Get(db.contentDBI, cKey); err == nil && len(val) == 8 {
				info.DocFreq = int(binary.BigEndian.Uint64(val))
			}
			results = append(results, info)
		}
		return nil
	})
	return results, err
}

// --- Strategy management ---

func (db *DB) AddStrategy(name, cmd string) error {
	db.settings.ChunkingStrategies[name] = cmd
	return db.saveSettings()
}

// CRC: crc-DB.md | R116, R117
func (db *DB) AddStrategyFunc(name string, fn ChunkFunc) error {
	db.funcStrategies[name] = fn
	db.settings.ChunkingStrategies[name] = "" // empty cmd marks func strategy
	return db.saveSettings()
}

func (db *DB) RemoveStrategy(name string) error {
	delete(db.settings.ChunkingStrategies, name)
	delete(db.funcStrategies, name)
	return db.saveSettings()
}

// --- Staleness ---

// Seq: seq-stale.md

// CheckFile checks whether an indexed file is fresh, stale, or missing on disk.
func (db *DB) CheckFile(fpath string) (FileStatus, error) {
	var status FileStatus
	status.Path = fpath

	err := db.env.View(func(txn *lmdb.Txn) error {
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.contentDBI, finalKey)
		if lmdb.IsNotFound(err) {
			return fmt.Errorf("file not indexed: %s", fpath)
		} else if err != nil {
			return err
		}
		fileid := binary.BigEndian.Uint64(val)
		status.FileID = fileid

		info, err := readFileInfo(txn, db.contentDBI, fileid)
		if err != nil {
			return err
		}
		status.Strategy = info.ChunkingStrategy
		status.Status = classifyFile(info)
		return nil
	})
	return status, err
}

// StaleFiles returns the status of every indexed file.
func (db *DB) StaleFiles() ([]FileStatus, error) {
	type fileEntry struct {
		fileid uint64
		info   FileInfo
	}
	var entries []fileEntry

	err := db.env.View(func(txn *lmdb.Txn) error {
		cursor, err := txn.OpenCursor(db.contentDBI)
		if err != nil {
			return err
		}
		defer cursor.Close()

		key, val, err := cursor.Get([]byte{prefixN}, nil, lmdb.SetRange)
		for err == nil && len(key) > 0 && key[0] == prefixN {
			if len(key) == 9 {
				fileid := binary.BigEndian.Uint64(key[1:])
				data := make([]byte, len(val))
				copy(data, val)
				var info FileInfo
				if jsonErr := json.Unmarshal(data, &info); jsonErr == nil {
					entries = append(entries, fileEntry{fileid, info})
				}
			}
			key, val, err = cursor.Get(nil, nil, lmdb.Next)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var statuses []FileStatus
	for _, e := range entries {
		statuses = append(statuses, FileStatus{
			Path:     e.info.Filename,
			FileID:   e.fileid,
			Strategy: e.info.ChunkingStrategy,
			Status:   classifyFile(e.info),
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
func classifyFile(info FileInfo) string {
	fi, err := os.Stat(info.Filename)
	if os.IsNotExist(err) {
		return "missing"
	}
	if err != nil {
		return "missing"
	}
	if fi.ModTime().UnixNano() == info.ModTime {
		return "fresh"
	}
	data, err := os.ReadFile(info.Filename)
	if err != nil {
		return "missing"
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) == info.ContentHash {
		return "fresh"
	}
	return "stale"
}

// --- Helpers ---

// scanCRecords cursor-scans all C[tri:3] records and returns (trigram, count) pairs.
func scanCRecords(txn *lmdb.Txn, dbi lmdb.DBI) []trigramCount {
	var result []trigramCount
	cursor, err := txn.OpenCursor(dbi)
	if err != nil {
		return result
	}
	defer cursor.Close()
	startKey := []byte{prefixC}
	key, val, err := cursor.Get(startKey, nil, lmdb.SetRange)
	for err == nil && len(key) == 4 && key[0] == prefixC {
		tri := uint32(key[1])<<16 | uint32(key[2])<<8 | uint32(key[3])
		if len(val) == 8 {
			c := binary.BigEndian.Uint64(val)
			if c > 0 {
				result = append(result, trigramCount{tri, c})
			}
		}
		key, val, err = cursor.Get(nil, nil, lmdb.Next)
	}
	return result
}

type trigramCount struct {
	tri   uint32
	count uint64
}

// computeActiveSlice scans C records, sorts by count, and returns the bottom pct% as a sorted trigram slice.
func computeActiveSlice(txn *lmdb.Txn, dbi lmdb.DBI, pct int) []uint32 {
	tcs := scanCRecords(txn, dbi)
	slices.SortFunc(tcs, func(a, b trigramCount) int {
		return cmp.Compare(a.count, b.count)
	})
	cutoffIdx := len(tcs) * pct / 100
	if cutoffIdx == 0 && len(tcs) > 0 {
		cutoffIdx = 1
	}
	active := make([]uint32, 0, cutoffIdx)
	for i := 0; i < cutoffIdx && i < len(tcs); i++ {
		active = append(active, tcs[i].tri)
	}
	slices.Sort(active) // sort by trigram value for binary search
	return active
}

// computeActiveSet returns a map for O(1) membership test of active trigrams.
func computeActiveSet(txn *lmdb.Txn, dbi lmdb.DBI, pct int) map[uint32]bool {
	active := computeActiveSlice(txn, dbi, pct)
	m := make(map[uint32]bool, len(active))
	for _, t := range active {
		m[t] = true
	}
	return m
}

// encodePackedTrigrams encodes a sorted trigram slice as packed 3-byte values.
func encodePackedTrigrams(trigrams []uint32) []byte {
	buf := make([]byte, len(trigrams)*3)
	for i, tri := range trigrams {
		off := i * 3
		buf[off] = byte(tri >> 16)
		buf[off+1] = byte(tri >> 8)
		buf[off+2] = byte(tri)
	}
	return buf
}

// decodePackedTrigrams decodes a packed 3-byte trigram list into a sorted []uint32.
func decodePackedTrigrams(data []byte) []uint32 {
	n := len(data) / 3
	result := make([]uint32, n)
	for i := 0; i < n; i++ {
		off := i * 3
		result[i] = uint32(data[off])<<16 | uint32(data[off+1])<<8 | uint32(data[off+2])
	}
	return result
}

// incrementCCount increments the sparse C record for a trigram, creating it if needed.
func incrementCCount(txn *lmdb.Txn, dbi lmdb.DBI, trigram uint32) error {
	key := makeCKey(trigram)
	var c uint64
	val, err := txn.Get(dbi, key)
	if err == nil && len(val) == 8 {
		c = binary.BigEndian.Uint64(val)
	}
	c++
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, c)
	return txn.Put(dbi, key, buf, 0)
}

// decrementCCount decrements the sparse C record for a trigram, deleting it if it reaches zero.
func decrementCCount(txn *lmdb.Txn, dbi lmdb.DBI, trigram uint32) {
	key := makeCKey(trigram)
	val, err := txn.Get(dbi, key)
	if err != nil || len(val) != 8 {
		return
	}
	c := binary.BigEndian.Uint64(val)
	if c <= 1 {
		txn.Del(dbi, key, nil)
		return
	}
	c--
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, c)
	txn.Put(dbi, key, buf, 0)
}

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
		return cmp.Compare(a.StartLine, b.StartLine)
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

func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func readFileInfo(txn *lmdb.Txn, dbi lmdb.DBI, fileid uint64) (FileInfo, error) {
	val, err := txn.Get(dbi, makeNKey(fileid))
	if err != nil {
		return FileInfo{}, err
	}
	data := make([]byte, len(val))
	copy(data, val)
	var info FileInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return FileInfo{}, err
	}
	return info, nil
}

// allocFileID reads the next file ID from the database and increments it atomically.
func (db *DB) allocFileID(txn *lmdb.Txn) (uint64, error) {
	// Re-read settings from DB to get the authoritative NextFileID
	val, err := txn.Get(db.contentDBI, []byte{prefixI})
	if err != nil {
		return 0, err
	}
	data := make([]byte, len(val))
	copy(data, val)
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return 0, err
	}
	fileid := s.NextFileID
	s.NextFileID++
	db.settings = s
	if err := putSettings(txn, db.contentDBI, &db.settings); err != nil {
		return 0, err
	}
	return fileid, nil
}

func byteAliasesToJSON(aliases map[byte]byte) map[string]string {
	if len(aliases) == 0 {
		return nil
	}
	m := make(map[string]string, len(aliases))
	for k, v := range aliases {
		m[string([]byte{k})] = string([]byte{v})
	}
	return m
}

func byteAliasesFromJSON(m map[string]string) map[byte]byte {
	if len(m) == 0 {
		return nil
	}
	aliases := make(map[byte]byte, len(m))
	for k, v := range m {
		if len(k) > 0 && len(v) > 0 {
			aliases[k[0]] = v[0]
		}
	}
	return aliases
}

func (db *DB) saveSettings() error {
	return db.env.Update(func(txn *lmdb.Txn) error {
		return putSettings(txn, db.contentDBI, &db.settings)
	})
}

func putSettings(txn *lmdb.Txn, dbi lmdb.DBI, s *Settings) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return txn.Put(dbi, []byte{prefixI}, data, 0)
}

func normalizeBoundaries(offsets []int64, fileLen int64) []int64 {
	seen := map[int64]bool{0: true, fileLen: true}
	bounds := []int64{0}
	for _, o := range offsets {
		if o > 0 && o < fileLen && !seen[o] {
			seen[o] = true
			bounds = append(bounds, o)
		}
	}
	bounds = append(bounds, fileLen)
	slices.Sort(bounds)
	return bounds
}

func computeChunkLines(data []byte, boundaries []int64) (startLines, endLines []int) {
	line := 1
	pos := int64(0)
	for i := 0; i < len(boundaries)-1; i++ {
		start := boundaries[i]
		end := boundaries[i+1]
		for pos < start {
			if data[pos] == '\n' {
				line++
			}
			pos++
		}
		startLine := line
		for pos < end {
			if data[pos] == '\n' {
				line++
			}
			pos++
		}
		endLine := line
		if end > start && data[end-1] == '\n' {
			endLine = line - 1
		}
		if endLine < startLine {
			endLine = startLine
		}
		startLines = append(startLines, startLine)
		endLines = append(endLines, endLine)
	}
	return
}
