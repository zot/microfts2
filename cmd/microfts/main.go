package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"microfts"
)

// CRC: crc-CLI.md

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
	case "build-index":
		cmdBuildIndex()
	case "strategy":
		cmdStrategy()
	case "stale":
		cmdStale()
	case "chunk-lines":
		cmdChunkLines()
	case "chunk-lines-overlap":
		cmdChunkLinesOverlap()
	case "chunk-words-overlap":
		cmdChunkWordsOverlap()
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
	fmt.Fprintln(os.Stderr, "commands: init, add, search, delete, reindex, build-index, strategy, stale,")
	fmt.Fprintln(os.Stderr, "          chunk-lines, chunk-lines-overlap, chunk-words-overlap")
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

func openOpts(contentDB, indexDB string) microfts.Options {
	return microfts.Options{
		ContentDBName: contentDB,
		IndexDBName:   indexDB,
	}
}

// --- Commands ---

func cmdInit() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	charset := fs.String("charset", "abcdefghijklmnopqrstuvwxyz0123456789", "character set")
	caseInsensitive := fs.Bool("case-insensitive", false, "case insensitive indexing")
	aliasStr := fs.String("aliases", "", "character aliases as from=to pairs, comma-separated (e.g. '\\n=^')")
	fs.Parse(os.Args[1:])

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "init: -db required")
		os.Exit(1)
	}

	aliases := parseAliases(*aliasStr)

	db, err := microfts.Create(*dbPath, microfts.Options{
		CharSet:       *charset,
		CaseInsensitive: *caseInsensitive,
		Aliases:       aliases,
		ContentDBName: *contentDB,
		IndexDBName:   *indexDB,
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

	db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	for _, fp := range fs.Args() {
		if err := db.AddFile(fp, *strategy); err != nil {
			fatal("add "+fp, err)
		}
	}
}

func cmdSearch() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	fs.Parse(os.Args[1:])

	if *dbPath == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "search: -db and query required")
		os.Exit(1)
	}

	query := strings.Join(fs.Args(), " ")

	db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	results, err := db.Search(query)
	if err != nil {
		fatal("search", err)
	}
	for _, r := range results {
		fmt.Printf("%s:%d-%d\n", r.Path, r.StartLine, r.EndLine)
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

	db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
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

	db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	for _, fp := range fs.Args() {
		if err := db.Reindex(fp, *strategy); err != nil {
			fatal("reindex "+fp, err)
		}
	}
}

func cmdBuildIndex() {
	fs := flag.CommandLine
	dbPath, contentDB, indexDB := dbFlags(fs)
	cutoff := fs.Int("cutoff", 50, "active trigram frequency percentile cutoff")
	fs.Parse(os.Args[1:])

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "build-index: -db required")
		os.Exit(1)
	}

	db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()
	doRefresh(db)

	if err := db.BuildIndex(*cutoff); err != nil {
		fatal("build-index", err)
	}
	fmt.Printf("index built (%d active trigrams)\n", len(db.Settings().ActiveTrigrams))
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

	db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
	if err != nil {
		fatal("open", err)
	}
	defer db.Close()

	refreshed, err := db.RefreshStale("")
	if err != nil {
		fatal("refresh", err)
	}
	for _, fs := range refreshed {
		fmt.Printf("%s\t%s\n", fs.Status, fs.Path)
	}
}

func doRefresh(db *microfts.DB) {
	if !globalRefresh {
		return
	}
	refreshed, err := db.RefreshStale("")
	if err != nil {
		fatal("refresh", err)
	}
	for _, fs := range refreshed {
		fmt.Fprintf(os.Stderr, "%s\t%s\n", fs.Status, fs.Path)
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

	db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
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
		db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
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
		db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
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
		db, err := microfts.Open(*dbPath, openOpts(*contentDB, *indexDB))
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

func parseAliases(s string) map[rune]rune {
	if s == "" {
		return nil
	}
	aliases := make(map[rune]rune)
	for _, pair := range strings.Split(s, ",") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		from := unescapeChar(strings.TrimSpace(parts[0]))
		to := unescapeChar(strings.TrimSpace(parts[1]))
		if from != 0 && to != 0 {
			aliases[from] = to
		}
	}
	return aliases
}

func unescapeChar(s string) rune {
	switch s {
	case `\n`:
		return '\n'
	case `\t`:
		return '\t'
	case `\r`:
		return '\r'
	default:
		runes := []rune(s)
		if len(runes) == 1 {
			return runes[0]
		}
		return 0
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

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fatal("chunk-lines", err)
	}
	defer f.Close()

	offset := int64(0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		offset += int64(len(scanner.Bytes())) + 1 // +1 for newline
		fmt.Println(offset)
	}
	if err := scanner.Err(); err != nil {
		fatal("chunk-lines", err)
	}
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

	// Find all line-start offsets
	starts := []int64{0}
	for i, b := range data {
		if b == '\n' && i+1 < len(data) {
			starts = append(starts, int64(i+1))
		}
	}

	step := max(*lines-*overlap, 1)
	for i := step; i < len(starts); i += step {
		fmt.Println(starts[i])
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
	for i := step; i < len(locs); i += step {
		fmt.Println(locs[i][0])
	}
}
