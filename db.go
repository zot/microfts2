package microfts

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/bmatsuo/lmdb-go/lmdb"
)

// CRC: crc-DB.md | Seq: seq-init.md, seq-add.md, seq-search.md, seq-build-index.md

// Record prefixes for content DB keys.
const (
	prefixC = 'C' // trigram counts (2MB)
	prefixI = 'I' // settings JSON
	prefixT = 'T' // trigram bitsets per chunk
	prefixN = 'N' // file info JSON
)

// countsSize is 262,144 trigrams × 8 bytes per count.
const countsSize = 262144 * 8

// DB is the main database handle.
type DB struct {
	env         *lmdb.Env
	contentDBI  lmdb.DBI
	indexDBI    lmdb.DBI
	indexExists bool
	settings    Settings
	charSet     *CharSet
	contentName string
	indexName   string
}

// Settings is stored as JSON in the I record.
type Settings struct {
	CharacterSet        string            `json:"characterSet"`
	CaseInsensitive     bool              `json:"caseInsensitive"`
	CharacterAliases    map[string]string  `json:"characterAliases,omitempty"`
	ChunkingStrategies  map[string]string  `json:"chunkingStrategies"`
	ActiveTrigramCutoff int               `json:"activeTrigramCutoff"`
	ActiveTrigrams      []uint32          `json:"activeTrigrams,omitempty"`
	NextFileID          uint64            `json:"nextFileID"`
}

// SearchResult is returned by Search.
type SearchResult struct {
	Path      string
	StartLine int
	EndLine   int
}

// FileInfo is stored as JSON in N records.
type FileInfo struct {
	Filename        string  `json:"filename"`
	ChunkOffsets    []int64 `json:"chunkOffsets"`
	ChunkStartLines []int   `json:"chunkStartLines"`
	ChunkEndLines   []int   `json:"chunkEndLines"`
	ChunkingStrategy string `json:"chunkingStrategy"`
}

// Options configures database creation and opening.
type Options struct {
	CharSet       string
	CaseInsensitive bool
	Aliases       map[rune]rune
	ContentDBName string
	IndexDBName   string
	MapSize       int64 // bytes, default 1GB
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

func (o *Options) mapSize() int64 {
	if o.MapSize > 0 {
		return o.MapSize
	}
	return 1 << 30
}

// --- Key construction ---

func makeTKey(fileid, chunknum uint64) []byte {
	key := make([]byte, 17)
	key[0] = prefixT
	binary.BigEndian.PutUint64(key[1:], fileid)
	binary.BigEndian.PutUint64(key[9:], chunknum)
	return key
}

func makeNKey(fileid uint64) []byte {
	key := make([]byte, 9)
	key[0] = prefixN
	binary.BigEndian.PutUint64(key[1:], fileid)
	return key
}

func makeIndexKey(trigram uint32, fileid, chunknum uint64) []byte {
	key := make([]byte, 19)
	key[0] = byte(trigram >> 16)
	key[1] = byte(trigram >> 8)
	key[2] = byte(trigram)
	binary.BigEndian.PutUint64(key[3:], fileid)
	binary.BigEndian.PutUint64(key[11:], chunknum)
	return key
}

func indexKeyTrigram(key []byte) uint32 {
	return uint32(key[0])<<16 | uint32(key[1])<<8 | uint32(key[2])
}

// --- Create / Open / Close ---

// Seq: seq-init.md
func Create(path string, opts Options) (*DB, error) {
	env, err := lmdb.NewEnv()
	if err != nil {
		return nil, fmt.Errorf("lmdb NewEnv: %w", err)
	}
	if err := env.SetMaxDBs(2); err != nil {
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

	charSet, err := NewCharSet(opts.CharSet, opts.CaseInsensitive, opts.Aliases)
	if err != nil {
		env.Close()
		return nil, err
	}

	settings := Settings{
		CharacterSet:        opts.CharSet,
		CaseInsensitive:     opts.CaseInsensitive,
		CharacterAliases:    aliasesToJSON(opts.Aliases),
		ChunkingStrategies:  make(map[string]string),
		ActiveTrigramCutoff: 50,
		NextFileID:          1,
	}

	db := &DB{
		env:         env,
		charSet:     charSet,
		contentName: opts.contentDB(),
		indexName:   opts.indexDB(),
		settings:    settings,
	}

	err = env.Update(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI(db.contentName, lmdb.Create)
		if err != nil {
			return err
		}
		db.contentDBI = dbi

		settingsJSON, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		if err := txn.Put(dbi, []byte{prefixI}, settingsJSON, 0); err != nil {
			return err
		}
		return txn.Put(dbi, []byte{prefixC}, make([]byte, countsSize), 0)
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
	if err := env.SetMaxDBs(2); err != nil {
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
		env:         env,
		contentName: opts.contentDB(),
		indexName:   opts.indexDB(),
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

		// Try opening index DB
		idbi, err := txn.OpenDBI(db.indexName, 0)
		if err == nil {
			db.indexDBI = idbi
			db.indexExists = true
		}
		return nil
	})
	if err != nil {
		env.Close()
		return nil, err
	}

	db.charSet, err = NewCharSet(db.settings.CharacterSet, db.settings.CaseInsensitive, aliasesFromJSON(db.settings.CharacterAliases))
	if err != nil {
		env.Close()
		return nil, err
	}

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

// --- AddFile ---

// Seq: seq-add.md
func (db *DB) AddFile(fpath, strategy string) error {
	cmd, ok := db.settings.ChunkingStrategies[strategy]
	if !ok {
		return fmt.Errorf("unknown chunking strategy: %s", strategy)
	}

	offsets, err := RunChunker(cmd, fpath)
	if err != nil {
		return fmt.Errorf("chunker: %w", err)
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}

	boundaries := normalizeBoundaries(offsets, int64(len(data)))
	startLines, endLines := computeChunkLines(data, boundaries)

	return db.env.Update(func(txn *lmdb.Txn) error {
		return db.addFileInTxn(txn, fpath, strategy, data, boundaries, startLines, endLines)
	})
}

func (db *DB) addFileInTxn(txn *lmdb.Txn, fpath, strategy string, data []byte, boundaries []int64, startLines, endLines []int) error {
	fileid, err := db.allocFileID(txn)
	if err != nil {
		return err
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
			return err
		}
	}

	counts, err := readCounts(txn, db.contentDBI)
	if err != nil {
		return err
	}

	// Process each chunk
	for i := 0; i < len(boundaries)-1; i++ {
		start := boundaries[i]
		end := boundaries[i+1]

		trigrams := db.charSet.Trigrams(string(data[start:end]))
		var bs Bitset
		for _, tri := range trigrams {
			bs.Set(tri)
		}

		bs.ForEach(func(tri uint32) {
			off := tri * 8
			c := binary.LittleEndian.Uint64(counts[off:])
			c++
			binary.LittleEndian.PutUint64(counts[off:], c)
		})

		if err := txn.Put(db.contentDBI, makeTKey(fileid, uint64(i)), bs.Bytes(), 0); err != nil {
			return err
		}
	}

	if err := txn.Put(db.contentDBI, []byte{prefixC}, counts, 0); err != nil {
		return err
	}

	info := FileInfo{
		Filename:         fpath,
		ChunkOffsets:     boundaries[:len(boundaries)-1],
		ChunkStartLines:  startLines,
		ChunkEndLines:    endLines,
		ChunkingStrategy: strategy,
	}
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := txn.Put(db.contentDBI, makeNKey(fileid), infoJSON, 0); err != nil {
		return err
	}

	return db.dropIndex(txn)
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

	info, err := readFileInfo(txn, db.contentDBI, fileid)
	if err != nil {
		return fmt.Errorf("file info for %s: %w", fpath, err)
	}

	counts, err := readCounts(txn, db.contentDBI)
	if err != nil {
		return err
	}

	for i := range info.ChunkOffsets {
		tKey := makeTKey(fileid, uint64(i))
		tVal, err := txn.Get(db.contentDBI, tKey)
		if err != nil {
			continue
		}
		var bs Bitset
		bs.FromBytes(tVal)
		bs.ForEach(func(tri uint32) {
			off := tri * 8
			c := binary.LittleEndian.Uint64(counts[off:])
			if c > 0 {
				c--
			}
			binary.LittleEndian.PutUint64(counts[off:], c)
		})
		if err := txn.Del(db.contentDBI, tKey, nil); err != nil {
			return err
		}
	}

	if err := txn.Put(db.contentDBI, []byte{prefixC}, counts, 0); err != nil {
		return err
	}
	if err := txn.Del(db.contentDBI, makeNKey(fileid), nil); err != nil {
		return err
	}

	for _, pair := range EncodeFilename(fpath) {
		txn.Del(db.contentDBI, pair.Key, nil)
	}

	return db.dropIndex(txn)
}

// --- Reindex ---

func (db *DB) Reindex(fpath, strategy string) error {
	cmd, ok := db.settings.ChunkingStrategies[strategy]
	if !ok {
		return fmt.Errorf("unknown chunking strategy: %s", strategy)
	}
	offsets, err := RunChunker(cmd, fpath)
	if err != nil {
		return fmt.Errorf("chunker: %w", err)
	}
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	boundaries := normalizeBoundaries(offsets, int64(len(data)))
	startLines, endLines := computeChunkLines(data, boundaries)

	return db.env.Update(func(txn *lmdb.Txn) error {
		if err := db.removeFileInTxn(txn, fpath); err != nil {
			return err
		}
		return db.addFileInTxn(txn, fpath, strategy, data, boundaries, startLines, endLines)
	})
}

// --- Search ---

// Seq: seq-search.md
func (db *DB) Search(query string) ([]SearchResult, error) {
	if !db.indexExists {
		if err := db.BuildIndex(db.settings.ActiveTrigramCutoff); err != nil {
			return nil, fmt.Errorf("build index: %w", err)
		}
	}

	queryTrigrams := db.charSet.Trigrams(query)
	if len(queryTrigrams) == 0 {
		return nil, nil
	}

	// Filter to unique active trigrams
	activeSet := make(map[uint32]bool, len(db.settings.ActiveTrigrams))
	for _, t := range db.settings.ActiveTrigrams {
		activeSet[t] = true
	}
	seen := make(map[uint32]bool)
	var active []uint32
	for _, t := range queryTrigrams {
		if activeSet[t] && !seen[t] {
			seen[t] = true
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return nil, nil
	}

	type chunkID struct{ fileid, chunknum uint64 }
	var results []SearchResult

	err := db.env.View(func(txn *lmdb.Txn) error {
		cursor, err := txn.OpenCursor(db.indexDBI)
		if err != nil {
			return err
		}
		defer cursor.Close()

		// Collect candidates from first trigram
		candidates := make(map[chunkID]bool)
		scanTrigram(cursor, active[0], func(fid, cnum uint64) {
			candidates[chunkID{fid, cnum}] = true
		})

		// Intersect with remaining trigrams
		for _, tri := range active[1:] {
			next := make(map[chunkID]bool)
			scanTrigram(cursor, tri, func(fid, cnum uint64) {
				id := chunkID{fid, cnum}
				if candidates[id] {
					next[id] = true
				}
			})
			candidates = next
		}

		// Resolve to results, caching FileInfo per fileid
		infoCache := make(map[uint64]*FileInfo)
		for id := range candidates {
			info, ok := infoCache[id.fileid]
			if !ok {
				fi, err := readFileInfo(txn, db.contentDBI, id.fileid)
				if err != nil {
					continue
				}
				info = &fi
				infoCache[id.fileid] = info
			}
			idx := int(id.chunknum)
			if idx < len(info.ChunkStartLines) && idx < len(info.ChunkEndLines) {
				results = append(results, SearchResult{
					Path:      info.Filename,
					StartLine: info.ChunkStartLines[idx],
					EndLine:   info.ChunkEndLines[idx],
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(results, func(a, b SearchResult) int {
		if a.Path != b.Path {
			if a.Path < b.Path {
				return -1
			}
			return 1
		}
		return a.StartLine - b.StartLine
	})
	return results, nil
}

func scanTrigram(cursor *lmdb.Cursor, trigram uint32, fn func(fileid, chunknum uint64)) {
	startKey := makeIndexKey(trigram, 0, 0)
	endTri := trigram + 1
	key, _, err := cursor.Get(startKey, nil, lmdb.SetRange)
	for err == nil && len(key) >= 19 {
		if indexKeyTrigram(key) >= endTri {
			break
		}
		fn(binary.BigEndian.Uint64(key[3:11]), binary.BigEndian.Uint64(key[11:19]))
		key, _, err = cursor.Get(nil, nil, lmdb.Next)
	}
}

// --- BuildIndex ---

// Seq: seq-build-index.md
func (db *DB) BuildIndex(cutoff int) error {
	return db.env.Update(func(txn *lmdb.Txn) error {
		// Read counts
		countsRaw, err := txn.Get(db.contentDBI, []byte{prefixC})
		if err != nil {
			return err
		}
		counts := make([]byte, len(countsRaw))
		copy(counts, countsRaw)

		// Collect non-zero trigram counts
		type tc struct {
			tri   uint32
			count uint64
		}
		var tcs []tc
		for i := uint32(0); i < 262144; i++ {
			c := binary.LittleEndian.Uint64(counts[i*8:])
			if c > 0 {
				tcs = append(tcs, tc{i, c})
			}
		}
		slices.SortFunc(tcs, func(a, b tc) int {
			if a.count < b.count {
				return -1
			}
			if a.count > b.count {
				return 1
			}
			return 0
		})

		// Bottom cutoff% are active
		cutoffIdx := len(tcs) * cutoff / 100
		if cutoffIdx == 0 && len(tcs) > 0 {
			cutoffIdx = 1
		}
		activeSet := make(map[uint32]bool, cutoffIdx)
		activeTrigrams := make([]uint32, 0, cutoffIdx)
		for i := 0; i < cutoffIdx && i < len(tcs); i++ {
			activeSet[tcs[i].tri] = true
			activeTrigrams = append(activeTrigrams, tcs[i].tri)
		}

		// Drop old index
		if db.indexExists {
			if err := txn.Drop(db.indexDBI, true); err != nil {
				return err
			}
		}

		// Create index DB
		indexDBI, err := txn.OpenDBI(db.indexName, lmdb.Create)
		if err != nil {
			return err
		}
		db.indexDBI = indexDBI

		// Iterate T records and populate index
		cursor, err := txn.OpenCursor(db.contentDBI)
		if err != nil {
			return err
		}
		defer cursor.Close()

		key, val, err := cursor.Get([]byte{prefixT}, nil, lmdb.SetRange)
		for err == nil && len(key) > 0 && key[0] == prefixT {
			if len(key) == 17 {
				fileid := binary.BigEndian.Uint64(key[1:9])
				chunknum := binary.BigEndian.Uint64(key[9:17])

				var bs Bitset
				bs.FromBytes(val)
				var putErr error
				bs.ForEach(func(tri uint32) {
					if putErr != nil {
						return
					}
					if activeSet[tri] {
						putErr = txn.Put(indexDBI, makeIndexKey(tri, fileid, chunknum), []byte{}, 0)
					}
				})
				if putErr != nil {
					return putErr
				}
			}
			key, val, err = cursor.Get(nil, nil, lmdb.Next)
		}

		// Update settings
		db.settings.ActiveTrigramCutoff = cutoff
		db.settings.ActiveTrigrams = activeTrigrams
		if err := putSettings(txn, db.contentDBI, &db.settings); err != nil {
			return err
		}

		db.indexExists = true
		return nil
	})
}

// --- Strategy management ---

func (db *DB) AddStrategy(name, cmd string) error {
	db.settings.ChunkingStrategies[name] = cmd
	return db.saveSettings()
}

func (db *DB) RemoveStrategy(name string) error {
	delete(db.settings.ChunkingStrategies, name)
	return db.saveSettings()
}

// --- Helpers ---

func readCounts(txn *lmdb.Txn, dbi lmdb.DBI) ([]byte, error) {
	raw, err := txn.Get(dbi, []byte{prefixC})
	if err != nil {
		return nil, err
	}
	counts := make([]byte, len(raw))
	copy(counts, raw)
	return counts, nil
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

func aliasesToJSON(aliases map[rune]rune) map[string]string {
	if len(aliases) == 0 {
		return nil
	}
	m := make(map[string]string, len(aliases))
	for k, v := range aliases {
		m[string(k)] = string(v)
	}
	return m
}

func aliasesFromJSON(m map[string]string) map[rune]rune {
	if len(m) == 0 {
		return nil
	}
	aliases := make(map[rune]rune, len(m))
	for k, v := range m {
		kr := []rune(k)
		vr := []rune(v)
		if len(kr) > 0 && len(vr) > 0 {
			aliases[kr[0]] = vr[0]
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

func (db *DB) dropIndex(txn *lmdb.Txn) error {
	if !db.indexExists {
		return nil
	}
	if err := txn.Drop(db.indexDBI, true); err != nil {
		return err
	}
	db.indexExists = false
	return nil
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
