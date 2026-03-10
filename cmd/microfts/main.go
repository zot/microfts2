package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"microfts2"
)

// CRC: crc-CLI.md

// stringSlice implements flag.Value for repeatable string flags. R194
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

var globalRefresh bool

func main() {
	globalRefresh, os.Args = extractRefreshFlag(os.Args)

	if len(os.Args) < 2 {
		if globalRefresh {
			fmt.Fprintln(os.Stderr, "-r requires -db flag")
			os.Exit(1)
		}
		usage()
	}

	cmd := os.Args[1]
	// If first arg is a flag, not a subcommand — standalone refresh mode
	if strings.HasPrefix(cmd, "-") {
		if globalRefresh {
			cmdRefreshOnly()
			return
		}
		usage()
	}

	os.Args = append(os.Args[:1], os.Args[2:]...)
	flag.CommandLine = flag.NewFlagSet(cmd, flag.ExitOnError)

	switch cmd {
	case "init":
		cmdInit()
	case "add":
		cmdAdd()
	case "search":
		cmdSearch()
	case "delete":
		cmdDelete()
	case "reindex":
		cmdReindex()
	case "strategy":
		cmdStrategy()
	case "stale":
		cmdStale()
	case "score":
		cmdScore()
	case "chunk-lines":
		cmdChunkLines()
	case "chunk-lines-overlap":
		cmdChunkLinesOverlap()
	case "chunk-words-overlap":
		cmdChunkWordsOverlap()
	case "chunk-markdown":
		cmdChunkMarkdown()
	case "chunks":
		cmdChunks()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
	}
}

func extractRefreshFlag(args []string) (bool, []string) {
	found := false
	var result []string
	for _, a := range args {
		if a == "-r" && !found {
			found = true
		} else {
			result = append(result, a)
		}
	}
	return found, result
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: microfts [-r] <command> [flags]")
	fmt.Fprintln(os.Stderr, "commands: init, add, search, delete, reindex, strategy, stale, score, chunks,")
	fmt.Fprintln(os.Stderr, "          chunk-lines, chunk-lines-overlap, chunk-words-overlap, chunk-markdown")
	fmt.Fprintln(os.Stderr, "flags:")
	fmt.Fprintln(os.Stderr, "  -r    refresh stale files before running command")
	os.Exit(1)
}

func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
	os.Exit(1)
}

// --- Shared flags ---

func dbFlags(fs *flag.FlagSet) (dbPath, contentDB, indexDB *string) {
	dbPath = fs.String("db", "", "database path (required)")
	contentDB = fs.String("content-db", "", "content subdatabase name")
	indexDB = fs.String("index-db", "", "index subdatabase name")
	return
}

func openOpts(contentDB, indexDB string) microfts2.Options {
	return microfts2.Options{
		ContentDBName: contentDB,
		IndexDBName:   indexDB,
	}
}

// --- Commands ---

func cmdInit() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	caseInsensitive := fs.Bool("case-insensitive", false, "case insensitive indexing")
	aliasStr := fs.String("aliases", "", "byte aliases as from=to pairs, comma-separated (e.g. '\\n=^')")
	fs.Parse(os.Args[1:])

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "init: -db required")
		os.Exit(1)
	}

	aliases := parseAliases(*aliasStr)

	db, err := microfts2.Create(*dbPath, microfts2.Options{
		CaseInsensitive: *caseInsensitive,
		Aliases:         aliases,
		ContentDBName:   *contentDB,
		IndexDBName:     *indexDB,
	})
	if err != nil {
		fatal("init", err)
	}
	db.Close()
	fmt.Println("database created")
}

func cmdAdd() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	strategy := fs.String("strategy", "", "chunking strategy name (required)")
	fs.Parse(os.Args[1:])

	if *dbPath == "" || *strategy == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "add: -db, -strategy, and at least one file required")
		os.Exit(1)
	}

	db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	for _, fp := range fs.Args() {
		if _, err := db.AddFile(fp, *strategy); err != nil {
			fatal("add "+fp, err)
		}
	}
}

func cmdSearch() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	useRegex := fs.Bool("regex", false, "treat query as a Go regexp pattern")
	scoreMode := fs.String("score", "coverage", "scoring strategy: coverage or density")
	verify := fs.Bool("verify", false, "post-filter: verify query terms in chunk text")
	var filterRegex, exceptRegex stringSlice
	fs.Var(&filterRegex, "filter-regex", "AND post-filter regex (repeatable)")
	fs.Var(&exceptRegex, "except-regex", "subtract post-filter regex (repeatable)")
	fs.Parse(os.Args[1:])

	if *dbPath == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "search: -db and query required")
		os.Exit(1)
	}

	var opt microfts2.SearchOption
	switch *scoreMode {
	case "coverage":
		opt = microfts2.WithCoverage()
	case "density":
		opt = microfts2.WithDensity()
	default:
		fmt.Fprintf(os.Stderr, "search: unknown score mode: %s\n", *scoreMode)
		os.Exit(1)
	}

	query := strings.Join(fs.Args(), " ")

	db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	var opts []microfts2.SearchOption
	opts = append(opts, opt)
	if *verify {
		opts = append(opts, microfts2.WithVerify())
	}
	if len(filterRegex) > 0 {
		opts = append(opts, microfts2.WithRegexFilter(filterRegex...))
	}
	if len(exceptRegex) > 0 {
		opts = append(opts, microfts2.WithExceptRegex(exceptRegex...))
	}

	var sr *microfts2.SearchResults
	if *useRegex {
		sr, err = db.SearchRegex(query, opts...)
	} else {
		sr, err = db.Search(query, opts...)
	}
	if err != nil {
		fatal("search", err)
	}
	for _, r := range sr.Results {
		fmt.Printf("%s:%s\n", r.Path, r.Range)
	}
}

func cmdDelete() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	fs.Parse(os.Args[1:])

	if *dbPath == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "delete: -db and at least one file required")
		os.Exit(1)
	}

	db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	for _, fp := range fs.Args() {
		if err := db.RemoveFile(fp); err != nil {
			fatal("delete "+fp, err)
		}
	}
}

func cmdReindex() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	strategy := fs.String("strategy", "", "new chunking strategy (required)")
	fs.Parse(os.Args[1:])

	if *dbPath == "" || *strategy == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "reindex: -db, -strategy, and at least one file required")
		os.Exit(1)
	}

	db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	for _, fp := range fs.Args() {
		if _, err := db.Reindex(fp, *strategy); err != nil {
			fatal("reindex "+fp, err)
		}
	}
}

func cmdRefreshOnly() {
	flag.CommandLine = flag.NewFlagSet("refresh", flag.ExitOnError)
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	fs.Parse(os.Args[1:])

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "-r: -db required")
		os.Exit(1)
	}

	db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()

	refreshed, err := db.RefreshStale("")
	if err != nil {
		fatal("refresh", err)
	}
	for _, s := range refreshed {
		fmt.Printf("%s\t%s\n", s.Status, s.Path)
	}
}

func doRefresh(db *microfts2.DB) {
	if !globalRefresh {
		return
	}
	refreshed, err := db.RefreshStale("")
	if err != nil {
		fatal("refresh", err)
	}
	for _, s := range refreshed {
		fmt.Fprintf(os.Stderr, "%s\t%s\n", s.Status, s.Path)
	}
}

func cmdStale() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	fs.Parse(os.Args[1:])

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "stale: -db required")
		os.Exit(1)
	}

	db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()

	statuses, err := db.StaleFiles()
	if err != nil {
		fatal("stale", err)
	}
	for _, s := range statuses {
		if s.Status != "fresh" {
			fmt.Printf("%s\t%s\n", s.Status, s.Path)
		}
	}
}

func cmdScore() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	scoreMode := fs.String("score", "coverage", "scoring strategy: coverage or density")
	fs.Parse(os.Args[1:])

	if *dbPath == "" || fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "score: -db, query, and at least one file required")
		os.Exit(1)
	}

	var fn microfts2.ScoreFunc
	switch *scoreMode {
	case "coverage":
		fn = microfts2.ScoreCoverage
	case "density":
		fn = microfts2.ScoreDensityFunc
	default:
		fmt.Fprintf(os.Stderr, "score: unknown score mode: %s\n", *scoreMode)
		os.Exit(1)
	}

	query := fs.Arg(0)
	files := fs.Args()[1:]

	db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	for _, fp := range files {
		chunks, err := db.ScoreFile(query, fp, fn)
		if err != nil {
			fatal("score "+fp, err)
		}
		for _, c := range chunks {
			fmt.Printf("%s:%s\t%.4f\n", fp, c.Range, c.Score)
		}
	}
}

func cmdStrategy() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "strategy: subcommand required (add, remove, list)")
		os.Exit(1)
	}
	sub := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)

	switch sub {
	case "add":
		name := fs.String("name", "", "strategy name")
		cmd := fs.String("cmd", "", "chunking command")
		fs.Parse(os.Args[1:])
		if *dbPath == "" || *name == "" || *cmd == "" {
			fmt.Fprintln(os.Stderr, "strategy add: -db, -name, -cmd required")
			os.Exit(1)
		}
		db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
		if err != nil {
			fatal("open", err)
		}
		defer db.Close()
		if err := db.AddStrategy(*name, *cmd); err != nil {
			fatal("strategy add", err)
		}

	case "remove":
		name := fs.String("name", "", "strategy name")
		fs.Parse(os.Args[1:])
		if *dbPath == "" || *name == "" {
			fmt.Fprintln(os.Stderr, "strategy remove: -db, -name required")
			os.Exit(1)
		}
		db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
		if err != nil {
			fatal("open", err)
		}
		defer db.Close()
		if err := db.RemoveStrategy(*name); err != nil {
			fatal("strategy remove", err)
		}

	case "list":
		fs.Parse(os.Args[1:])
		if *dbPath == "" {
			fmt.Fprintln(os.Stderr, "strategy list: -db required")
			os.Exit(1)
		}
		db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
		if err != nil {
			fatal("open", err)
		}
		defer db.Close()
		for name, cmd := range db.Settings().ChunkingStrategies {
			fmt.Printf("%s: %s\n", name, cmd)
		}

	default:
		fmt.Fprintf(os.Stderr, "strategy: unknown subcommand %s\n", sub)
		os.Exit(1)
	}
}

func parseAliases(s string) map[byte]byte {
	if s == "" {
		return nil
	}
	aliases := make(map[byte]byte)
	for _, pair := range strings.Split(s, ",") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		from := unescapeByte(strings.TrimSpace(parts[0]))
		to := unescapeByte(strings.TrimSpace(parts[1]))
		if from >= 0 && to >= 0 {
			aliases[byte(from)] = byte(to)
		}
	}
	return aliases
}

func unescapeByte(s string) int {
	switch s {
	case `\n`:
		return int('\n')
	case `\t`:
		return int('\t')
	case `\r`:
		return int('\r')
	default:
		if len(s) == 1 {
			return int(s[0])
		}
		return -1
	}
}

// Seq: seq-chunks.md | R204, R205
func cmdChunks() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	before := fs.Int("before", 0, "number of chunks before target")
	after := fs.Int("after", 0, "number of chunks after target")
	fs.Parse(os.Args[1:])

	if *dbPath == "" || fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "chunks: -db, <file>, and <range> required")
		os.Exit(1)
	}

	fpath := fs.Arg(0)
	targetRange := fs.Arg(1)

	db, err := microfts2.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	chunks, err := db.GetChunks(fpath, targetRange, *before, *after)
	if err != nil {
		fatal("chunks", err)
	}

	enc := json.NewEncoder(os.Stdout)
	for _, c := range chunks {
		enc.Encode(c)
	}
}

// --- Built-in chunkers ---

func cmdChunkLines() {
	fs := flag.CommandLine
	fs.Parse(os.Args[1:])
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "chunk-lines: file required")
		os.Exit(1)
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fatal("chunk-lines", err)
	}

	microfts2.LineChunkFunc(fs.Arg(0), data, func(c microfts2.Chunk) bool {
		fmt.Printf("%s\t%s", c.Range, c.Content)
		return true
	})
}

func cmdChunkLinesOverlap() {
	fs := flag.CommandLine
	lines := fs.Int("lines", 50, "lines per chunk")
	overlap := fs.Int("overlap", 10, "lines of overlap")
	fs.Parse(os.Args[1:])
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "chunk-lines-overlap: file required")
		os.Exit(1)
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fatal("chunk-lines-overlap", err)
	}

	// Find all line-start byte offsets and count lines
	lineStarts := []int{0}
	for i, b := range data {
		if b == '\n' && i+1 < len(data) {
			lineStarts = append(lineStarts, i+1)
		}
	}
	totalLines := len(lineStarts)

	step := max(*lines-*overlap, 1)
	for startLine := 0; startLine < totalLines; startLine += step {
		endLine := startLine + *lines
		if endLine > totalLines {
			endLine = totalLines
		}
		startByte := lineStarts[startLine]
		var endByte int
		if endLine < totalLines {
			endByte = lineStarts[endLine]
		} else {
			endByte = len(data)
		}
		content := data[startByte:endByte]
		fmt.Printf("%d-%d\t%s\n", startLine+1, endLine, strings.ReplaceAll(string(content), "\n", "\\n"))
	}
}

func cmdChunkWordsOverlap() {
	fs := flag.CommandLine
	words := fs.Int("words", 200, "words per chunk")
	overlap := fs.Int("overlap", 50, "words of overlap")
	pattern := fs.String("pattern", `\S+`, "regexp defining a word")
	fs.Parse(os.Args[1:])
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "chunk-words-overlap: file required")
		os.Exit(1)
	}

	re, err := regexp.Compile(*pattern)
	if err != nil {
		fatal("chunk-words-overlap: invalid pattern", err)
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fatal("chunk-words-overlap", err)
	}

	// Find byte offset of each word start
	locs := re.FindAllIndex(data, -1)
	if len(locs) == 0 {
		return
	}

	step := max(*words-*overlap, 1)
	chunkNum := 1
	for i := 0; i < len(locs); i += step {
		endIdx := i + *words
		if endIdx > len(locs) {
			endIdx = len(locs)
		}
		startByte := locs[i][0]
		endByte := locs[endIdx-1][1]
		content := data[startByte:endByte]
		fmt.Printf("w%d\t%s\n", chunkNum, strings.ReplaceAll(string(content), "\n", "\\n"))
		chunkNum++
	}
}

// CRC: crc-CLI.md | R175
func cmdChunkMarkdown() {
	fs := flag.CommandLine
	fs.Parse(os.Args[1:])
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "chunk-markdown: file required")
		os.Exit(1)
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fatal("chunk-markdown", err)
	}

	microfts2.MarkdownChunkFunc(fs.Arg(0), data, func(c microfts2.Chunk) bool {
		fmt.Printf("%s\t%s", c.Range, c.Content)
		return true
	})
}
