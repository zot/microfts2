package microfts2

// CRC: crc-Overlay.md | Seq: seq-tmp-add.md, seq-tmp-search.md

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode/utf8"
)

// overlay holds in-memory tmp:// documents. R349, R353
type overlay struct {
	mu          sync.RWMutex
	nextFileID  uint64 // counts down from MaxUint64, R351
	nextChunkID uint64 // counts down from MaxUint64, R352
	files       map[string]*overlayFile
	filesByID   map[uint64]*overlayFile
	chunks      map[uint64]*overlayChunk
	trigrams    map[uint32]map[uint64]struct{} // trigram → chunkid set
	bigrams     map[uint16]map[uint64]struct{} // bigram → chunkid set, R398
	tokens      map[uint32]map[uint64]struct{} // token hash → chunkid set
	hashes      map[[32]byte]uint64            // content hash → chunkid, R354
	totalChunks int                            // R373
	totalTokens int                            // R373
}

// overlayFile is the in-memory equivalent of an FRecord. R371
type overlayFile struct {
	fileID   uint64
	path     string
	content  []byte // stored for chunk retrieval
	strategy string
	chunks   []FileChunkEntry
	tokens   []TokenEntry
}

// overlayChunk is the in-memory equivalent of a CRecord.
type overlayChunk struct {
	chunkID  uint64
	hash     [32]byte
	trigrams []TrigramEntry
	bigrams  []BigramEntry // R398: when bigrams enabled
	tokens   []TokenEntry
	attrs    []Pair
	fileIDs  []uint64
}

func newOverlay() *overlay {
	return &overlay{
		nextFileID:  math.MaxUint64,
		nextChunkID: math.MaxUint64,
		files:       make(map[string]*overlayFile),
		filesByID:   make(map[uint64]*overlayFile),
		chunks:      make(map[uint64]*overlayChunk),
		trigrams:    make(map[uint32]map[uint64]struct{}),
		bigrams:     make(map[uint16]map[uint64]struct{}),
		tokens:      make(map[uint32]map[uint64]struct{}),
		hashes:      make(map[[32]byte]uint64),
	}
}

// addFile indexes a tmp:// document in the overlay. R358, R359, R360
func (o *overlay) addFile(path, strategy string, content []byte, db *DB) (uint64, error) {
	if !utf8.Valid(content) {
		return 0, fmt.Errorf("tmp content is not valid UTF-8: %s", path)
	}
	chunker := db.resolveChunker(strategy)
	if chunker == nil {
		return 0, fmt.Errorf("chunking strategy %q not registered", strategy)
	}

	// Collect chunks outside the lock (chunking is CPU work).
	collected, err := collectChunksFromContent(path, content, chunker, db)
	if err != nil {
		return 0, err
	}
	if len(collected) == 0 {
		return 0, fmt.Errorf("%w: %s", ErrNoChunks, path)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if _, exists := o.files[path]; exists {
		return 0, ErrAlreadyIndexed
	}

	fileID := o.nextFileID
	o.nextFileID--

	ofile := &overlayFile{
		fileID:   fileID,
		path:     path,
		content:  append([]byte(nil), content...),
		strategy: strategy,
	}

	o.populateFileChunksLocked(ofile, collected)
	o.files[path] = ofile
	o.filesByID[fileID] = ofile
	return fileID, nil
}

// updateFile replaces a tmp:// document's content. R361, R362, R363
func (o *overlay) updateFile(path, strategy string, content []byte, db *DB) error {
	if !utf8.Valid(content) {
		return fmt.Errorf("tmp content is not valid UTF-8: %s", path)
	}
	chunker := db.resolveChunker(strategy)
	if chunker == nil {
		return fmt.Errorf("chunking strategy %q not registered", strategy)
	}
	collected, err := collectChunksFromContent(path, content, chunker, db)
	if err != nil {
		return err
	}
	if len(collected) == 0 {
		return fmt.Errorf("%w: %s", ErrNoChunks, path)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	old, exists := o.files[path]
	if !exists {
		return fmt.Errorf("tmp file not found: %s", path)
	}

	// Remove old chunks, repopulate with new ones (same fileID). R362
	o.removeFileChunksLocked(old)
	old.content = append([]byte(nil), content...)
	old.strategy = strategy
	old.chunks = nil
	old.tokens = nil
	o.populateFileChunksLocked(old, collected)
	return nil
}

// populateFileChunksLocked dedup-creates chunks and builds the file's chunk list
// and token bag. Must hold write lock.
func (o *overlay) populateFileChunksLocked(ofile *overlayFile, collected []collectedChunk) {
	var fileTokMap map[string]int
	for _, cc := range collected {
		chunkID := o.dedupOrCreateChunk(cc, ofile.fileID)
		ofile.chunks = append(ofile.chunks, FileChunkEntry{
			ChunkID:  chunkID,
			Location: cc.rangeStr,
		})
		if fileTokMap == nil {
			fileTokMap = make(map[string]int)
		}
		for _, te := range cc.tokens {
			fileTokMap[te.Token] += te.Count
		}
	}
	for tok, cnt := range fileTokMap {
		ofile.tokens = append(ofile.tokens, TokenEntry{Token: tok, Count: cnt})
	}
}

// removeFile removes a tmp:// document. R364, R365
func (o *overlay) removeFile(path string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	ofile, exists := o.files[path]
	if !exists {
		return fmt.Errorf("tmp file not found: %s", path)
	}

	o.removeFileChunksLocked(ofile)
	delete(o.files, path)
	delete(o.filesByID, ofile.fileID)
	return nil
}

// removeFileChunksLocked removes a file's association from all its chunks,
// cleaning up orphaned chunks. Must hold write lock.
func (o *overlay) removeFileChunksLocked(ofile *overlayFile) {
	for _, fce := range ofile.chunks {
		oc, ok := o.chunks[fce.ChunkID]
		if !ok {
			continue
		}
		// Remove this fileID from the chunk.
		remaining := make([]uint64, 0, len(oc.fileIDs))
		for _, fid := range oc.fileIDs {
			if fid != ofile.fileID {
				remaining = append(remaining, fid)
			}
		}
		if len(remaining) == 0 {
			// Orphaned — delete chunk and clean up indices.
			for _, te := range oc.trigrams {
				if set, ok := o.trigrams[te.Trigram]; ok {
					delete(set, fce.ChunkID)
					if len(set) == 0 {
						delete(o.trigrams, te.Trigram)
					}
				}
			}
			for _, te := range oc.tokens {
				th := tokenHash(te.Token)
				if set, ok := o.tokens[th]; ok {
					delete(set, fce.ChunkID)
					if len(set) == 0 {
						delete(o.tokens, th)
					}
				}
			}
			// R398: clean up bigram index for orphaned chunk
			for _, be := range oc.bigrams {
				if set, ok := o.bigrams[be.Bigram]; ok {
					delete(set, fce.ChunkID)
					if len(set) == 0 {
						delete(o.bigrams, be.Bigram)
					}
				}
			}
			delete(o.hashes, oc.hash)
			delete(o.chunks, fce.ChunkID)
			o.totalChunks--
			o.totalTokens -= len(oc.tokens)
		} else {
			oc.fileIDs = remaining
		}
	}
}

// dedupOrCreateChunk checks for hash dedup, creates if new. Must hold write lock.
func (o *overlay) dedupOrCreateChunk(cc collectedChunk, fileID uint64) uint64 {
	if existing, ok := o.hashes[cc.hash]; ok {
		oc := o.chunks[existing]
		oc.fileIDs = append(oc.fileIDs, fileID)
		return existing
	}

	chunkID := o.nextChunkID
	o.nextChunkID--

	// Build TrigramEntry slice and populate trigram index.
	var trigEntries []TrigramEntry
	for tri, cnt := range cc.triCounts {
		trigEntries = append(trigEntries, TrigramEntry{Trigram: tri, Count: cnt})
		set, ok := o.trigrams[tri]
		if !ok {
			set = make(map[uint64]struct{})
			o.trigrams[tri] = set
		}
		set[chunkID] = struct{}{}
	}

	// Populate token index.
	for _, te := range cc.tokens {
		th := tokenHash(te.Token)
		set, ok := o.tokens[th]
		if !ok {
			set = make(map[uint64]struct{})
			o.tokens[th] = set
		}
		set[chunkID] = struct{}{}
	}

	// R398: build bigram entries and populate bigram index
	var bigramEntries []BigramEntry
	if cc.biCounts != nil {
		bigramEntries = make([]BigramEntry, 0, len(cc.biCounts))
		for bi, cnt := range cc.biCounts {
			bigramEntries = append(bigramEntries, BigramEntry{Bigram: bi, Count: cnt})
			set, ok := o.bigrams[bi]
			if !ok {
				set = make(map[uint64]struct{})
				o.bigrams[bi] = set
			}
			set[chunkID] = struct{}{}
		}
	}

	oc := &overlayChunk{
		chunkID:  chunkID,
		hash:     cc.hash,
		trigrams: trigEntries,
		bigrams:  bigramEntries,
		tokens:   append([]TokenEntry(nil), cc.tokens...),
		attrs:    copyPairs(cc.attrs),
		fileIDs:  []uint64{fileID},
	}
	o.chunks[chunkID] = oc
	o.hashes[cc.hash] = chunkID
	o.totalChunks++
	o.totalTokens += len(cc.tokens)
	return chunkID
}

// collectChunksFromContent runs the chunker and extracts trigrams/tokens.
// Pure computation — no overlay state accessed.
func collectChunksFromContent(path string, content []byte, chunker Chunker, db *DB) ([]collectedChunk, error) {
	var chunks []collectedChunk
	var utf8Err error
	if err := chunker.Chunks(path, content, func(c Chunk) bool {
		if !utf8.Valid(c.Content) {
			utf8Err = fmt.Errorf("chunk %q contains invalid UTF-8 in %s", c.Range, path)
			return false
		}
		h := sha256.Sum256(c.Content)
		cc := collectedChunk{
			rangeStr:  string(c.Range),
			hash:      h,
			triCounts: db.trigrams.TrigramCounts(c.Content),
			tokens:    tokenizeCounts(c.Content),
		}
		if db.settings.BigramsEnabled { // R398
			cc.biCounts = db.trigrams.BigramCounts(c.Content)
		}
		cc.attrs = copyPairs(c.Attrs)
		chunks = append(chunks, cc)
		return true
	}); err != nil {
		return nil, err
	}
	if utf8Err != nil {
		return nil, utf8Err
	}
	return chunks, nil
}

// searchOverlay collects overlay candidates, scores them, and resolves to SearchResults.
// Single lock acquisition for the entire search path. R366
func (o *overlay) searchOverlay(termTrigrams [][]uint32, active []uint32, fuzzy bool, scoreFunc ScoreFunc, cfg searchConfig) []SearchResult {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if len(o.files) == 0 {
		return nil
	}

	// Collect candidate chunkIDs.
	activeSet := make(map[uint32]bool, len(active))
	for _, t := range active {
		activeSet[t] = true
	}

	var candidateIDs map[uint64]bool
	if fuzzy {
		candidateIDs = make(map[uint64]bool)
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
			for id := range o.intersectTrigramsLocked(termActive) {
				candidateIDs[id] = true
			}
		}
	} else {
		candidateIDs = o.intersectTrigramsLocked(active)
	}

	if len(candidateIDs) == 0 {
		return nil
	}

	// Collect, filter, score, and resolve — all under one lock.
	var results []SearchResult
	for cid := range candidateIDs {
		oc, ok := o.chunks[cid]
		if !ok {
			continue
		}

		crec := CRecord{
			ChunkID: oc.chunkID,
			Hash:    oc.hash,
			Attrs:   oc.attrs,
			FileIDs: oc.fileIDs,
			Trigrams: oc.trigrams,
		}
		if !applyChunkFilters(crec, cfg) {
			continue
		}

		var counts map[uint32]int
		if cfg.bigramScore {
			if len(oc.bigrams) > 0 {
				counts = make(map[uint32]int, len(oc.bigrams))
				for _, be := range oc.bigrams {
					counts[uint32(be.Bigram)] = be.Count
				}
			}
			// nil counts when no bigram data → score 0
		} else {
			counts = make(map[uint32]int, len(oc.trigrams))
			for _, te := range oc.trigrams {
				counts[te.Trigram] = te.Count
			}
		}

		var score float64
		if active == nil {
			score = 1.0
		} else {
			score = scoreFunc(active, counts, len(oc.tokens))
			if score <= 0 {
				continue
			}
		}

		for _, fid := range oc.fileIDs {
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
			ofile, ok := o.filesByID[fid]
			if !ok {
				continue
			}
			for _, fce := range ofile.chunks {
				if fce.ChunkID == cid {
					results = append(results, SearchResult{
						Path:  ofile.path,
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

// searchOverlayAll returns results for all overlay chunks (used by SearchRegex). R366
func (o *overlay) searchOverlayAll(_ ScoreFunc, cfg searchConfig) []SearchResult {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if len(o.files) == 0 {
		return nil
	}

	var results []SearchResult
	for _, oc := range o.chunks {
		crec := CRecord{
			ChunkID: oc.chunkID,
			Hash:    oc.hash,
			Attrs:   oc.attrs,
			FileIDs: oc.fileIDs,
			Trigrams: oc.trigrams,
		}
		if !applyChunkFilters(crec, cfg) {
			continue
		}

		score := 1.0 // regex search scores all candidates equally
		for _, fid := range oc.fileIDs {
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
			ofile, ok := o.filesByID[fid]
			if !ok {
				continue
			}
			for _, fce := range ofile.chunks {
				if fce.ChunkID == oc.chunkID {
					results = append(results, SearchResult{
						Path:  ofile.path,
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

// intersectTrigramsLocked does AND intersection within trigrams. Must hold RLock.
func (o *overlay) intersectTrigramsLocked(trigrams []uint32) map[uint64]bool {
	if len(trigrams) == 0 {
		return nil
	}
	var result map[uint64]bool
	for i, tri := range trigrams {
		set := o.trigrams[tri]
		if i == 0 {
			result = make(map[uint64]bool, len(set))
			for id := range set {
				result[id] = true
			}
		} else {
			next := make(map[uint64]bool)
			for id := range set {
				if result[id] {
					next[id] = true
				}
			}
			result = next
		}
		if len(result) == 0 {
			return nil
		}
	}
	return result
}

// tmpFileIDs returns the set of all overlay fileids. R369
func (o *overlay) tmpFileIDs() map[uint64]struct{} {
	o.mu.RLock()
	defer o.mu.RUnlock()

	ids := make(map[uint64]struct{}, len(o.filesByID))
	for fid := range o.filesByID {
		ids[fid] = struct{}{}
	}
	return ids
}

// counters returns the overlay's totalChunks and totalTokens. R373
func (o *overlay) counters() (int, int) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.totalChunks, o.totalTokens
}

// lookupFileByPath returns an overlay file by path, or nil.
func (o *overlay) lookupFileByPath(path string) *overlayFile {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.files[path]
}

// isTmpPath returns true if the path has a tmp:// prefix. R350
func isTmpPath(path string) bool {
	return strings.HasPrefix(path, "tmp://")
}

// trigramDFs returns document frequencies for multiple trigrams in one lock acquisition. R374
func (o *overlay) trigramDFs(trigrams []uint32) []int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	dfs := make([]int, len(trigrams))
	for i, tri := range trigrams {
		dfs[i] = len(o.trigrams[tri])
	}
	return dfs
}

// empty returns true if the overlay has no documents.
func (o *overlay) empty() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.files) == 0
}
