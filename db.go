package microfts2

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
			// Count varint-encoded chunkids in the value
			ids, _ := UnmarshalTValue(val)
			count = len(ids)
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

// appendToTRecord adds a chunkid to a T record (read-modify-write).
func appendToTRecord(th TxnHolder, dbi lmdb.DBI, trigram uint32, chunkid uint64) error {
	txn := th.Txn()
	key := makeTKey(trigram)
	var existing []byte
	val, err := txn.Get(dbi, key)
	if err == nil {
		existing = make([]byte, len(val))
		copy(existing, val)
	} else if !lmdb.IsNotFound(err) {
		return err
	}
	// Append the new chunkid
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], chunkid)
	newVal := append(existing, buf[:n]...)
	return txn.Put(dbi, key, newVal, 0)
}

// appendToWRecord adds a chunkid to a W record (read-modify-write).
func appendToWRecord(th TxnHolder, dbi lmdb.DBI, tokHash uint32, chunkid uint64) error {
	txn := th.Txn()
	key := makeWKey(tokHash)
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

// batchAppendT appends a chunkid to multiple T records.
func batchAppendT(th TxnHolder, dbi lmdb.DBI, trigrams []uint32, chunkid uint64) error {
	for _, tri := range trigrams {
		if err := appendToTRecord(th, dbi, tri, chunkid); err != nil {
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
		if err := appendToWRecord(th, dbi, h, chunkid); err != nil {
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
		h := sha256.Sum256(c.Content)
		chunks = append(chunks, collectedChunk{
			rangeStr:  string(c.Range),
			hash:      h,
			triCounts: db.trigrams.TrigramCounts(c.Content),
			tokens:    tokenizeCounts(c.Content),
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

// Seq: seq-add.md | R213, R214, R223-R226, R233-R240, R253
func (db *DB) addFileInTxn(th TxnHolder, fpath, strategy string, chunks []collectedChunk, modTime int64, hash string, fileLength int64) (uint64, error) {
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

	// Accumulate trigrams and tokens per new chunkid for batch T/W updates
	type newChunkTW struct {
		chunkid  uint64
		trigrams []uint32
		tokens   []TokenEntry
	}
	var newChunks []newChunkTW

	for i, ch := range chunks {
		// Check H record for dedup
		hKey := makeHKey(ch.hash)
		var chunkid uint64

		hVal, err := txn.Get(db.dbi, hKey)
		if err == nil {
			// Dedup hit — chunk already exists, add our fileid to its C record
			chunkid, _ = readUvarint(hVal)
			// Read existing C record, add fileid
			cKey := makeCKey(chunkid)
			cVal, err := txn.Get(db.dbi, cKey)
			if err != nil {
				return 0, fmt.Errorf("read C record %d: %w", chunkid, err)
			}
			cData := make([]byte, len(cVal))
			copy(cData, cVal)
			crec, err := UnmarshalCValue(cData)
			if err != nil {
				return 0, err
			}
			crec.FileIDs = append(crec.FileIDs, fileid)
			if err := txn.Put(db.dbi, cKey, crec.MarshalValue(), 0); err != nil {
				return 0, err
			}
			// No T/W updates needed — chunk already indexed
		} else if lmdb.IsNotFound(err) {
			// New chunk — allocate chunkid, create H and C records
			chunkid, err = db.allocChunkID(th)
			if err != nil {
				return 0, err
			}

			// Write H record
			var hValBuf [binary.MaxVarintLen64]byte
			hn := binary.PutUvarint(hValBuf[:], chunkid)
			if err := txn.Put(db.dbi, hKey, hValBuf[:hn], 0); err != nil {
				return 0, err
			}

			// Build trigram entries
			triEntries := make([]TrigramEntry, 0, len(ch.triCounts))
			triList := make([]uint32, 0, len(ch.triCounts))
			for tri, cnt := range ch.triCounts {
				triEntries = append(triEntries, TrigramEntry{Trigram: tri, Count: cnt})
				triList = append(triList, tri)
			}

			// Create C record
			crec := CRecord{
				ChunkID:  chunkid,
				Hash:     ch.hash,
				Trigrams: triEntries,
				Tokens:   ch.tokens,
				FileIDs:  []uint64{fileid},
			}
			if err := txn.Put(db.dbi, makeCKey(chunkid), crec.MarshalValue(), 0); err != nil {
				return 0, err
			}

			// Accumulate for batch T/W
			newChunks = append(newChunks, newChunkTW{
				chunkid:  chunkid,
				trigrams: triList,
				tokens:   ch.tokens,
			})
		} else {
			return 0, err
		}

		fileChunks[i] = FileChunkEntry{ChunkID: chunkid, Location: ch.rangeStr}
		mergeTokenBag(fileBag, ch.tokens)
	}

	// Batch T record updates — one read-modify-write per affected trigram
	for _, nc := range newChunks {
		if err := batchAppendT(th, db.dbi, nc.trigrams, nc.chunkid); err != nil {
			return 0, err
		}
	}

	// Batch W record updates — one read-modify-write per affected token hash
	for _, nc := range newChunks {
		if err := batchAppendW(th, db.dbi, nc.tokens, nc.chunkid); err != nil {
			return 0, err
		}
	}

	// Write F record
	var contentHashBytes [32]byte
	hb, _ := hex.DecodeString(hash)
	copy(contentHashBytes[:], hb)

	frec := FRecord{
		FileID:      fileid,
		ModTime:     modTime,
		ContentHash: contentHashBytes,
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
	// Look up fileid from N records
	finalKey := FinalKey(fpath)
	val, err := txn.Get(db.dbi, finalKey)
	if lmdb.IsNotFound(err) {
		return fmt.Errorf("file not found: %s", fpath)
	} else if err != nil {
		return fmt.Errorf("lookup %s: %w", fpath, err)
	}
	// Parse N-255 value: [name-len:varint][name:str][fileid:varint]
	_, n := readString(val)
	fileid, _ := readUvarint(val[n:])

	// Read F record
	frec, err := db.readFRecord(txnWrap{txn}, fileid)
	if err != nil {
		return fmt.Errorf("read F record for %s: %w", fpath, err)
	}

	// For each chunk: remove fileid from C record, clean up orphans
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
			txn.Del(db.dbi, cKey, nil)
			txn.Del(db.dbi, makeHKey(crec.Hash), nil)

			// Remove chunkid from T records
			for _, te := range crec.Trigrams {
				removeFromTRecord(th, db.dbi, te.Trigram, fce.ChunkID)
			}
			// Remove chunkid from W records
			seen := make(map[uint32]bool)
			for _, te := range crec.Tokens {
				h := tokenHash(te.Token)
				if seen[h] {
					continue
				}
				seen[h] = true
				removeFromWRecord(th, db.dbi, h, fce.ChunkID)
			}
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

// removeFromTRecord removes a chunkid from a T record value.
func removeFromTRecord(th TxnHolder, dbi lmdb.DBI, trigram uint32, chunkid uint64) {
	txn := th.Txn()
	key := makeTKey(trigram)
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
		tr := TRecord{ChunkIDs: newIDs}
		txn.Put(dbi, key, tr.MarshalValue(), 0)
	}
}

// removeFromWRecord removes a chunkid from a W record value.
func removeFromWRecord(th TxnHolder, dbi lmdb.DBI, tokHash uint32, chunkid uint64) {
	txn := th.Txn()
	key := makeWKey(tokHash)
	val, err := txn.Get(dbi, key)
	if err != nil {
		return
	}
	ids, _ := UnmarshalWValue(val)
	var newIDs []uint64
	for _, id := range ids {
		if id != chunkid {
			newIDs = append(newIDs, id)
		}
	}
	if len(newIDs) == 0 {
		txn.Del(dbi, key, nil)
	} else {
		wr := WRecord{ChunkIDs: newIDs}
		txn.Put(dbi, key, wr.MarshalValue(), 0)
	}
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

		type newChunkTW struct {
			chunkid  uint64
			trigrams []uint32
			tokens   []TokenEntry
		}
		var newChunksTW []newChunkTW

		for _, ch := range newChunks {
			// Chunk dedup via H records (same as addFileInTxn)
			hKey := makeHKey(ch.hash)
			var chunkid uint64

			hVal, err := txn.Get(db.dbi, hKey)
			if err == nil {
				// Dedup hit — chunk already exists, add our fileid to its C record
				chunkid, _ = readUvarint(hVal)
				cKey := makeCKey(chunkid)
				cVal, err := txn.Get(db.dbi, cKey)
				if err != nil {
					return fmt.Errorf("read C record %d: %w", chunkid, err)
				}
				cData := make([]byte, len(cVal))
				copy(cData, cVal)
				crec, err := UnmarshalCValue(cData)
				if err != nil {
					return err
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
						return err
					}
				}
			} else if lmdb.IsNotFound(err) {
				// New chunk
				chunkid, err = db.allocChunkID(th)
				if err != nil {
					return err
				}

				// Write H record
				var hValBuf [binary.MaxVarintLen64]byte
				hn := binary.PutUvarint(hValBuf[:], chunkid)
				if err := txn.Put(db.dbi, hKey, hValBuf[:hn], 0); err != nil {
					return err
				}

				// Build trigram entries
				triEntries := make([]TrigramEntry, 0, len(ch.triCounts))
				triList := make([]uint32, 0, len(ch.triCounts))
				for tri, cnt := range ch.triCounts {
					triEntries = append(triEntries, TrigramEntry{Trigram: tri, Count: cnt})
					triList = append(triList, tri)
				}

				// Create C record
				crec := CRecord{
					ChunkID:  chunkid,
					Hash:     ch.hash,
					Trigrams: triEntries,
					Tokens:   ch.tokens,
					FileIDs:  []uint64{fileid},
				}
				if err := txn.Put(db.dbi, makeCKey(chunkid), crec.MarshalValue(), 0); err != nil {
					return err
				}

				newChunksTW = append(newChunksTW, newChunkTW{
					chunkid:  chunkid,
					trigrams: triList,
					tokens:   ch.tokens,
				})
			} else {
				return err
			}

			frec.Chunks = append(frec.Chunks, FileChunkEntry{ChunkID: chunkid, Location: ch.rangeStr})
			mergeTokenBag(fileBag, ch.tokens)
		}

		// Batch T/W record updates
		for _, nc := range newChunksTW {
			if err := batchAppendT(th, db.dbi, nc.trigrams, nc.chunkid); err != nil {
				return err
			}
		}
		for _, nc := range newChunksTW {
			if err := batchAppendW(th, db.dbi, nc.tokens, nc.chunkid); err != nil {
				return err
			}
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
		th := txnWrap{txn}
		active := selectQueryTrigrams(th, db.dbi, queryTrigrams, cfg)
		if len(active) == 0 {
			return nil
		}
		activeSet := make(map[uint32]bool, len(active))
		for _, t := range active {
			activeSet[t] = true
		}

		// Read T records for each term's active trigrams, intersect chunkid sets
		var candidateChunks map[uint64]bool
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
			if candidateChunks == nil {
				candidateChunks = termChunks
			} else {
				candidateChunks = intersectChunkSets(candidateChunks, termChunks)
			}
			if len(candidateChunks) == 0 {
				return nil
			}
		}

		// Read C records for surviving chunkids, score, resolve to results
		results = db.scoreAndResolve(th, candidateChunks, active, cfg)
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

// scoreAndResolve reads C records for candidate chunkids, scores them, and resolves to SearchResults.
func (db *DB) scoreAndResolve(th TxnHolder, candidates map[uint64]bool, active []uint32, cfg searchConfig) []SearchResult {
	txn := th.Txn()
	var results []SearchResult
	activeSet := make(map[uint32]bool, len(active))
	for _, t := range active {
		activeSet[t] = true
	}

	// Cache F records to avoid re-reading for same file
	frecCache := make(map[uint64]*FRecord)

	for chunkid := range candidates {
		// Read C record
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
		crec.db = db
		crec.txn = txn

		// Apply ChunkFilters
		if !applyChunkFilters(crec, cfg) {
			continue
		}

		// Build chunkCounts map from C record trigrams (only active ones)
		chunkCounts := make(map[uint32]int, len(crec.Trigrams))
		for _, te := range crec.Trigrams {
			chunkCounts[te.Trigram] = te.Count
		}

		// Token count from C record
		tokenCount := len(crec.Tokens)

		// Score — when active is nil (e.g., regex search), assign 1.0
		var score float64
		if active == nil {
			score = 1.0
		} else {
			score = cfg.scoreFunc(active, chunkCounts, tokenCount)
			if score <= 0 {
				continue
			}
		}

		// Resolve: for each fileid, find this chunk's location in the F record
		for _, fid := range crec.FileIDs {
			// File ID filter (WithOnly/WithExcept)
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

			// Find the chunk's location in the F record
			for _, fce := range frec.Chunks {
				if fce.ChunkID == chunkid {
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
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.dbi, finalKey)
		if err != nil {
			return err
		}
		_, n := readString(val)
		fileid, _ := readUvarint(val[n:])
		frec, err := db.readFRecord(txnWrap{txn}, fileid)
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

		results = db.scoreAndResolve(th, candidates, nil, cfg)
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
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.dbi, finalKey)
		if err != nil {
			return fmt.Errorf("file not found: %s", fpath)
		}
		_, n := readString(val)
		fileid, _ := readUvarint(val[n:])
		frec, err = db.readFRecord(txnWrap{txn}, fileid)
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
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.dbi, finalKey)
		if err != nil {
			return fmt.Errorf("file not found: %s", fpath)
		}
		_, n := readString(val)
		fileid, _ := readUvarint(val[n:])

		frec, err := db.readFRecord(th, fileid)
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
		finalKey := FinalKey(fpath)
		val, err := txn.Get(db.dbi, finalKey)
		if lmdb.IsNotFound(err) {
			return fmt.Errorf("file not indexed: %s", fpath)
		} else if err != nil {
			return err
		}
		_, n := readString(val)
		fileid, _ := readUvarint(val[n:])
		status.FileID = fileid

		frec, err := db.readFRecord(txnWrap{txn}, fileid)
		if err != nil {
			return err
		}
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

func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
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

