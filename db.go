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

// chunkID identifies a specific chunk within a file.
type chunkID struct{ fileid, chunknum uint64 }

// collectedChunk holds processed chunk data between generator collection and DB write.
type collectedChunk struct {
	rangeStr   string
	triCounts  map[uint32]int
	tokenCount int
}

// prefixR is the reverse index prefix in the index DB (legacy, pending rewrite).
const prefixR = 'R'

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

// FileInfo is stored as JSON in N records.
// R148
type FileInfo struct {
	Filename         string   `json:"filename"`
	ChunkRanges      []string `json:"chunkRanges"`
	ChunkTokenCounts []int    `json:"chunkTokenCounts"`
	ChunkingStrategy string   `json:"chunkingStrategy"`
	ModTime          int64    `json:"modTime"`
	ContentHash      string   `json:"contentHash"`
	FileLength       int64    `json:"fileLength,omitempty"`
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

func makeNKey(fileid uint64) []byte {
	key := make([]byte, 9)
	key[0] = prefixN
	binary.BigEndian.PutUint64(key[1:], fileid)
	return key
}

// makeOldCKey builds a C record key: C[trigram:3] = 4 bytes.
func makeOldCKey(trigram uint32) []byte {
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

// lookupTrigramCounts reads C records for deduplicated query trigrams and returns their corpus counts.
func lookupTrigramCounts(txn *lmdb.Txn, dbi lmdb.DBI, queryTrigrams []uint32) []TrigramCount {
	seen := make(map[uint32]bool)
	var result []TrigramCount
	for _, t := range queryTrigrams {
		if seen[t] {
			continue
		}
		seen[t] = true
		key := makeOldCKey(t)
		var count int
		val, err := txn.Get(dbi, key)
		if err == nil && len(val) == 8 {
			count = int(binary.BigEndian.Uint64(val))
		}
		result = append(result, TrigramCount{Trigram: t, Count: count})
	}
	return result
}

// countTotalChunks scans N records and sums up the chunk count across all files.
func countTotalChunks(txn *lmdb.Txn, dbi lmdb.DBI) int {
	total := 0
	cursor, err := txn.OpenCursor(dbi)
	if err != nil {
		return 0
	}
	defer cursor.Close()
	key, val, err := cursor.Get([]byte{prefixN}, nil, lmdb.SetRange)
	for err == nil && len(key) > 0 && key[0] == prefixN {
		if len(key) == 9 {
			data := make([]byte, len(val))
			copy(data, val)
			var info FileInfo
			if json.Unmarshal(data, &info) == nil {
				total += len(info.ChunkRanges)
			}
		}
		key, val, err = cursor.Get(nil, nil, lmdb.Next)
	}
	return total
}

// applyTrigramFilter uses the caller-supplied filter to select query trigrams.
// Returns the selected trigram codes as a []uint32.
func applyTrigramFilter(txn *lmdb.Txn, contentDBI lmdb.DBI, queryTrigrams []uint32, filter TrigramFilter) []uint32 {
	counts := lookupTrigramCounts(txn, contentDBI, queryTrigrams)
	total := countTotalChunks(txn, contentDBI)
	selected := filter(counts, total)
	result := make([]uint32, len(selected))
	for i, tc := range selected {
		result[i] = tc.Trigram
	}
	return result
}

// selectQueryTrigrams uses the caller-supplied filter (or FilterAll) to select query trigrams.
func selectQueryTrigrams(txn *lmdb.Txn, contentDBI lmdb.DBI, queryTrigrams []uint32, cfg searchConfig) []uint32 {
	filter := cfg.trigramFilter
	if filter == nil {
		filter = FilterAll
	}
	return applyTrigramFilter(txn, contentDBI, queryTrigrams, filter)
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

// --- I record helpers (data-in-key settings) ---

// iGet reads a single I record value. Returns ("", nil) if not found.
func iGet(txn *lmdb.Txn, dbi lmdb.DBI, name string) (string, error) {
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
func iPut(txn *lmdb.Txn, dbi lmdb.DBI, name, value string) error {
	return txn.Put(dbi, makeIKey(name), []byte(value), 0)
}

// iDel deletes a single I record.
func iDel(txn *lmdb.Txn, dbi lmdb.DBI, name string) error {
	err := txn.Del(dbi, makeIKey(name), nil)
	if lmdb.IsNotFound(err) {
		return nil
	}
	return err
}

// iCounter reads a counter I record as uint64. Returns 0 if not found.
func iCounter(txn *lmdb.Txn, dbi lmdb.DBI, name string) (uint64, error) {
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
func iSetCounter(txn *lmdb.Txn, dbi lmdb.DBI, name string, v uint64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return txn.Put(dbi, makeIKey(name), buf[:], 0)
}

// writeSettings writes all settings as individual I records.
func writeSettings(txn *lmdb.Txn, dbi lmdb.DBI, s *Settings) error {
	ci := "false"
	if s.CaseInsensitive {
		ci = "true"
	}
	if err := iPut(txn, dbi, "caseInsensitive", ci); err != nil {
		return err
	}
	for from, to := range s.Aliases {
		key := fmt.Sprintf("alias:%c", from)
		if err := iPut(txn, dbi, key, string([]byte{to})); err != nil {
			return err
		}
	}
	for name, cmd := range s.ChunkingStrategies {
		key := "strategy:" + name
		if err := iPut(txn, dbi, key, cmd); err != nil {
			return err
		}
	}
	return nil
}

// loadSettings reads all settings from I records. Uses a cursor to scan the I prefix range.
func loadSettings(txn *lmdb.Txn, dbi lmdb.DBI) (Settings, error) {
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

		if err := writeSettings(txn, dbi, &settings); err != nil {
			return err
		}
		// Initialize counters
		if err := iSetCounter(txn, dbi, "nextFileID", 1); err != nil {
			return err
		}
		return iSetCounter(txn, dbi, "nextChunkID", 1)
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

		s, err := loadSettings(txn, dbi)
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
		result = lookupTrigramCounts(txn, db.dbi, rawTrigrams)
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
func (db *DB) collectChunks(fpath, strategy string) ([]collectedChunk, []byte, int64, string, error) {
	if _, ok := db.settings.ChunkingStrategies[strategy]; !ok {
		return nil, nil, 0, "", fmt.Errorf("unknown chunking strategy: %s", strategy)
	}

	modTime, err := fileModTime(fpath)
	if err != nil {
		return nil, nil, 0, "", err
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil, nil, 0, "", err
	}

	chunkFn := db.resolveChunkFunc(strategy)
	if chunkFn == nil {
		return nil, nil, 0, "", fmt.Errorf("func strategy %q not registered (re-register with AddStrategyFunc after Open)", strategy)
	}

	var chunks []collectedChunk
	var utf8Err error
	if err := chunkFn(fpath, data, func(c Chunk) bool {
		if !utf8.Valid(c.Content) {
			utf8Err = fmt.Errorf("chunk %q contains invalid UTF-8 in %s", c.Range, fpath)
			return false
		}
		chunks = append(chunks, collectedChunk{
			rangeStr:   string(c.Range),
			triCounts:  db.trigrams.TrigramCounts(c.Content),
			tokenCount: countTokens(c.Content),
		})
		return true
	}); err != nil {
		return nil, nil, 0, "", err
	}
	if utf8Err != nil {
		return nil, nil, 0, "", utf8Err
	}
	if len(chunks) == 0 {
		return nil, nil, 0, "", fmt.Errorf("%w: %s", ErrNoChunks, fpath)
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
		fileid, txnErr = db.addFileInTxn(txn, fpath, strategy, chunks, modTime, hash, int64(len(data)))
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

// Seq: seq-add.md | R213, R214
func (db *DB) addFileInTxn(txn *lmdb.Txn, fpath, strategy string, chunks []collectedChunk, modTime int64, hash string, fileLength int64) (uint64, error) {
	// Dedup guard: check for existing F records before allocating a fileid
	finalKey := FinalKey(fpath)
	_, err := txn.Get(db.dbi, finalKey)
	if err == nil {
		return 0, ErrAlreadyIndexed
	} else if !lmdb.IsNotFound(err) {
		return 0, fmt.Errorf("check existing %s: %w", fpath, err)
	}

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
		if err := txn.Put(db.dbi, pair.Key, val, 0); err != nil {
			return 0, err
		}
	}

	// Process each chunk — write forward index entries
	rChunks := make(map[uint64][]chunkTrigramEntry)
	chunkRanges := make([]string, len(chunks))
	tokenCounts := make([]int, len(chunks))

	for i, ch := range chunks {
		chunknum := uint64(i)
		chunkRanges[i] = ch.rangeStr
		tokenCounts[i] = ch.tokenCount

		var entries []chunkTrigramEntry
		for tri, cnt := range ch.triCounts {
			// Update sparse C record
			if err := incrementCCount(txn, db.dbi, tri); err != nil {
				return 0, err
			}

			// Clamp count for index key
			idxCount := uint16(cnt)
			if cnt > 65535 {
				idxCount = 65535
			}

			// Write forward index entry
			if err := txn.Put(db.dbi, makeIndexKey(tri, idxCount, fileid, chunknum), []byte{}, 0); err != nil {
				return 0, err
			}

			entries = append(entries, chunkTrigramEntry{tri, idxCount})
		}
		rChunks[chunknum] = entries
	}

	// Write R record (reverse index)
	if err := txn.Put(db.dbi, makeRKey(fileid), encodeRValue(rChunks), 0); err != nil {
		return 0, err
	}

	// Write N record
	// R146, R147
	info := FileInfo{
		Filename:         fpath,
		ChunkRanges:      chunkRanges,
		ChunkTokenCounts: tokenCounts,
		ChunkingStrategy: strategy,
		ModTime:          modTime,
		ContentHash:      hash,
		FileLength:       fileLength,
	}
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return 0, err
	}
	return fileid, txn.Put(db.dbi, makeNKey(fileid), infoJSON, 0)
}

// --- RemoveFile ---

func (db *DB) RemoveFile(fpath string) error {
	return db.env.Update(func(txn *lmdb.Txn) error {
		return db.removeFileInTxn(txn, fpath)
	})
}

func (db *DB) removeFileInTxn(txn *lmdb.Txn, fpath string) error {
	finalKey := FinalKey(fpath)
	val, err := txn.Get(db.dbi, finalKey)
	if lmdb.IsNotFound(err) {
		return fmt.Errorf("file not found: %s", fpath)
	} else if err != nil {
		return fmt.Errorf("lookup %s: %w", fpath, err)
	}
	fileid := binary.BigEndian.Uint64(val)

	// Read R record to find all forward index entries
	rKey := makeRKey(fileid)
	rVal, err := txn.Get(db.dbi, rKey)
	if err != nil && !lmdb.IsNotFound(err) {
		return fmt.Errorf("read R record for %s: %w", fpath, err)
	}

	if rVal != nil {
		entries := decodeRValue(slices.Clone(rVal))
		for _, e := range entries {
			// Delete forward index entry
			txn.Del(db.dbi, makeIndexKey(e.trigram, e.count, fileid, e.chunknum), nil)

			// Decrement sparse C record
			decrementCCount(txn, db.dbi, e.trigram)
		}

		// Delete R record
		txn.Del(db.dbi, rKey, nil)
	}

	if err := txn.Del(db.dbi, makeNKey(fileid), nil); err != nil {
		return err
	}

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
		if err := db.removeFileInTxn(txn, fpath); err != nil {
			return err
		}
		var txnErr error
		fileid, txnErr = db.addFileInTxn(txn, fpath, strategy, chunks, modTime, hash, int64(len(data)))
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

	// Read existing file info and R record
	var info FileInfo
	var existingRData []byte
	err := db.env.View(func(txn *lmdb.Txn) error {
		var err error
		info, err = readFileInfo(txn, db.dbi, fileid)
		if err != nil {
			if lmdb.IsNotFound(err) {
				return fmt.Errorf("fileid %d not found", fileid)
			}
			return err
		}
		rVal, err := txn.Get(db.dbi, makeRKey(fileid))
		if err != nil && !lmdb.IsNotFound(err) {
			return err
		}
		if rVal != nil {
			existingRData = make([]byte, len(rVal))
			copy(existingRData, rVal)
		}
		return nil
	})
	if err != nil {
		return err
	}

	existingChunkCount := len(info.ChunkRanges)

	// Chunk the appended content
	var newChunks []collectedChunk
	var utf8Err error
	if err := chunkFn(info.Filename, content, func(c Chunk) bool {
		if !utf8.Valid(c.Content) {
			utf8Err = fmt.Errorf("chunk %q contains invalid UTF-8", c.Range)
			return false
		}
		newChunks = append(newChunks, collectedChunk{
			rangeStr:   string(c.Range),
			triCounts:  db.trigrams.TrigramCounts(c.Content),
			tokenCount: countTokens(c.Content),
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
		newRChunks := make(map[uint64][]chunkTrigramEntry)

		for i, ch := range newChunks {
			chunknum := uint64(existingChunkCount + i)

			var entries []chunkTrigramEntry
			for tri, cnt := range ch.triCounts {
				// Increment C record (R156)
				if err := incrementCCount(txn, db.dbi, tri); err != nil {
					return err
				}

				idxCount := uint16(cnt)
				if cnt > 65535 {
					idxCount = 65535
				}

				// Write forward index entry (R154)
				if err := txn.Put(db.dbi, makeIndexKey(tri, idxCount, fileid, chunknum), []byte{}, 0); err != nil {
					return err
				}

				entries = append(entries, chunkTrigramEntry{tri, idxCount})
			}
			newRChunks[chunknum] = entries
		}

		// Replace R record: existing data + new chunk groups (R155)
		// Byte append is safe: R records are self-describing chunk groups
		// ([chunknum:8][numTri:2][[tri:3][count:2]]...) with no header/footer.
		newRData := encodeRValue(newRChunks)
		combinedR := append(existingRData, newRData...)
		if err := txn.Put(db.dbi, makeRKey(fileid), combinedR, 0); err != nil {
			return err
		}

		// Update N record (R157)
		for _, ch := range newChunks {
			info.ChunkRanges = append(info.ChunkRanges, ch.rangeStr)
			info.ChunkTokenCounts = append(info.ChunkTokenCounts, ch.tokenCount)
		}
		if cfg.contentHash != "" {
			info.ContentHash = cfg.contentHash
		}
		if cfg.modTime != 0 {
			info.ModTime = cfg.modTime
		}
		if cfg.hasFileLength {
			info.FileLength = cfg.fileLength
		}

		infoJSON, err := json.Marshal(info)
		if err != nil {
			return err
		}
		return txn.Put(db.dbi, makeNKey(fileid), infoJSON, 0)
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

// chunkCandidate tracks accumulated trigram counts for a candidate chunk.
type chunkCandidate struct {
	counts map[uint32]int
}

// Seq: seq-search.md | R178, R179, R180, R181, R182
func (db *DB) Search(query string, opts ...SearchOption) (*SearchResults, error) {
	cfg := applySearchOpts(opts)

	query = strings.TrimSpace(query)
	terms := parseQueryTerms(query)
	if len(terms) == 0 {
		return &SearchResults{Status: IndexStatus{Built: true}}, nil
	}

	// Extract trigrams per term; union for filter, per-term for candidate collection
	termTrigrams := make([][]uint32, len(terms))
	trigramSet := make(map[uint32]bool)
	for i, term := range terms {
		tris := db.trigrams.ExtractTrigrams([]byte(term))
		termTrigrams[i] = tris
		for _, t := range tris {
			trigramSet[t] = true
		}
	}
	if len(trigramSet) == 0 {
		return &SearchResults{Status: IndexStatus{Built: true}}, nil
	}
	queryTrigrams := make([]uint32, 0, len(trigramSet))
	for t := range trigramSet {
		queryTrigrams = append(queryTrigrams, t)
	}

	var results []SearchResult

	err := db.env.View(func(txn *lmdb.Txn) error {
		active := selectQueryTrigrams(txn, db.dbi, queryTrigrams, cfg)
		if len(active) == 0 {
			return nil
		}
		activeSet := make(map[uint32]bool, len(active))
		for _, t := range active {
			activeSet[t] = true
		}

		cursor, err := txn.OpenCursor(db.dbi)
		if err != nil {
			return err
		}
		defer cursor.Close()

		// Collect and intersect candidates per term, then across terms
		var candidates map[chunkID]*chunkCandidate
		for _, tris := range termTrigrams {
			// Filter to only active trigrams for this term
			var termActive []uint32
			for _, t := range tris {
				if activeSet[t] {
					termActive = append(termActive, t)
				}
			}
			if len(termActive) == 0 {
				continue
			}
			termCandidates := collectCandidates(cursor, termActive)
			if candidates == nil {
				candidates = termCandidates
			} else {
				candidates = intersectCandidates(candidates, termCandidates)
			}
			if len(candidates) == 0 {
				return nil
			}
		}

		results = resolveResults(txn, db.dbi, candidates, active, cfg)
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

	sortResults(results)
	return &SearchResults{
		Results: results,
		Status:  IndexStatus{Built: true},
	}, nil
}

// collectCandidates scans the index for each trigram, intersecting to find
// chunks that contain all given trigrams. Returns candidates with their counts.
func collectCandidates(cursor *lmdb.Cursor, trigrams []uint32) map[chunkID]*chunkCandidate {
	if len(trigrams) == 0 {
		return nil
	}
	candidates := make(map[chunkID]*chunkCandidate)
	scanTrigram(cursor, trigrams[0], func(fid, cnum uint64, count uint16) {
		id := chunkID{fid, cnum}
		cc := &chunkCandidate{counts: make(map[uint32]int)}
		cc.counts[trigrams[0]] = int(count)
		candidates[id] = cc
	})
	for _, tri := range trigrams[1:] {
		if len(candidates) == 0 {
			break
		}
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
	return candidates
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
	var info FileInfo
	err := db.env.View(func(txn *lmdb.Txn) error {
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.dbi, finalKey)
		if err != nil {
			return err
		}
		fileid := binary.BigEndian.Uint64(val)
		info, err = readFileInfo(txn, db.dbi, fileid)
		return err
	})
	if err != nil {
		return nil
	}

	chunkFn := db.resolveChunkFunc(info.ChunkingStrategy)
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
		if idx < len(info.ChunkRanges) {
			tokenCount := 0
			if idx < len(info.ChunkTokenCounts) {
				tokenCount = info.ChunkTokenCounts[idx]
			}
			score := cfg.scoreFunc(active, cc.counts, tokenCount)
			results = append(results, SearchResult{
				Path:  info.Filename,
				Range: info.ChunkRanges[idx],
				Score: score,
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
		cursor, err := txn.OpenCursor(db.dbi)
		if err != nil {
			return err
		}
		defer cursor.Close()

		candidates := evalTrigramQuery(q, cursor, db.trigrams)
		if candidates == nil {
			// QAll: match everything — scan all N records for chunk IDs
			candidates = allChunks(txn, db.dbi)
		}

		results = resolveResults(txn, db.dbi, candidates, nil, cfg)
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
				for i := range info.ChunkRanges {
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
		info, err = readFileInfo(txn, db.dbi, fileid)
		return err
	})
	return info, err
}

// --- GetChunks ---

// Seq: seq-chunks.md | R197, R198, R199, R200, R201, R202, R203
// GetChunks retrieves the target chunk (identified by range label) and
// up to before/after positional neighbors. Returns chunks in positional order.
func (db *DB) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error) {
	var info FileInfo
	err := db.env.View(func(txn *lmdb.Txn) error {
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.dbi, finalKey)
		if err != nil {
			return fmt.Errorf("file not found: %s", fpath)
		}
		fileid := binary.BigEndian.Uint64(val)
		info, err = readFileInfo(txn, db.dbi, fileid)
		return err
	})
	if err != nil {
		return nil, err
	}

	// Find target chunk index by exact range label match.
	targetIdx := -1
	for i, r := range info.ChunkRanges {
		if r == targetRange {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return nil, fmt.Errorf("range %q not found in %s", targetRange, fpath)
	}

	// Compute window clamped to bounds.
	lo := max(0, targetIdx-before)
	hi := min(len(info.ChunkRanges)-1, targetIdx+after)

	// Re-chunk the file to recover content.
	chunkFn := db.resolveChunkFunc(info.ChunkingStrategy)
	if chunkFn == nil {
		return nil, fmt.Errorf("chunking strategy %q not registered", info.ChunkingStrategy)
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
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.dbi, finalKey)
		if err != nil {
			return fmt.Errorf("file not found: %s", fpath)
		}
		fileid := binary.BigEndian.Uint64(val)

		info, err := readFileInfo(txn, db.dbi, fileid)
		if err != nil {
			return err
		}

		active := selectQueryTrigrams(txn, db.dbi, queryTrigrams, cfg)
		if len(active) == 0 {
			return nil
		}

		// Read R record to get per-chunk trigram counts
		rKey := makeRKey(fileid)
		rVal, err := txn.Get(db.dbi, rKey)
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

		for i := range info.ChunkRanges {
			counts := chunkCounts[uint64(i)]
			if counts == nil {
				counts = make(map[uint32]int)
			}
			tokenCount := 0
			if i < len(info.ChunkTokenCounts) {
				tokenCount = info.ChunkTokenCounts[i]
			}
			results = append(results, ScoredChunk{
				Range: info.ChunkRanges[i],
				Score: fn(active, counts, tokenCount),
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
		return iPut(txn, db.dbi, "strategy:"+name, cmd)
	})
}

// CRC: crc-DB.md | R116, R117
func (db *DB) AddStrategyFunc(name string, fn ChunkFunc) error {
	db.funcStrategies[name] = fn
	db.settings.ChunkingStrategies[name] = "" // empty cmd marks func strategy
	return db.env.Update(func(txn *lmdb.Txn) error {
		return iPut(txn, db.dbi, "strategy:"+name, "")
	})
}

func (db *DB) RemoveStrategy(name string) error {
	delete(db.settings.ChunkingStrategies, name)
	delete(db.funcStrategies, name)
	return db.env.Update(func(txn *lmdb.Txn) error {
		return iDel(txn, db.dbi, "strategy:"+name)
	})
}

// --- Staleness ---

// Seq: seq-stale.md

// CheckFile checks whether an indexed file is fresh, stale, or missing on disk.
func (db *DB) CheckFile(fpath string) (FileStatus, error) {
	var status FileStatus
	status.Path = fpath

	err := db.env.View(func(txn *lmdb.Txn) error {
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.dbi, finalKey)
		if lmdb.IsNotFound(err) {
			return fmt.Errorf("file not indexed: %s", fpath)
		} else if err != nil {
			return err
		}
		fileid := binary.BigEndian.Uint64(val)
		status.FileID = fileid

		info, err := readFileInfo(txn, db.dbi, fileid)
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
		cursor, err := txn.OpenCursor(db.dbi)
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

// incrementCCount increments the sparse C record for a trigram, creating it if needed.
func incrementCCount(txn *lmdb.Txn, dbi lmdb.DBI, trigram uint32) error {
	key := makeOldCKey(trigram)
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
	key := makeOldCKey(trigram)
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

// allocFileID reads the next file ID counter and increments it atomically.
func (db *DB) allocFileID(txn *lmdb.Txn) (uint64, error) {
	id, err := iCounter(txn, db.dbi, "nextFileID")
	if err != nil {
		return 0, err
	}
	if id == 0 {
		id = 1 // should not happen after Create, but be safe
	}
	if err := iSetCounter(txn, db.dbi, "nextFileID", id+1); err != nil {
		return 0, err
	}
	return id, nil
}

// allocChunkID reads the next chunk ID counter and increments it atomically.
func (db *DB) allocChunkID(txn *lmdb.Txn) (uint64, error) {
	id, err := iCounter(txn, db.dbi, "nextChunkID")
	if err != nil {
		return 0, err
	}
	if id == 0 {
		id = 1
	}
	if err := iSetCounter(txn, db.dbi, "nextChunkID", id+1); err != nil {
		return 0, err
	}
	return id, nil
}

func (db *DB) saveSettings() error {
	return db.env.Update(func(txn *lmdb.Txn) error {
		return writeSettings(txn, db.dbi, &db.settings)
	})
}

