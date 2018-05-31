package cmd

import (
	"flag"
	"fmt"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"github.com/logv/sybil/src/sybil"
)

var MAX_RECORDS_NO_GC = 4 * 1000 * 1000 // 4 million

var NO_RECYCLE_MEM *bool

const (
	SORT_COUNT = "$COUNT"
)

func addQueryFlags(flags *sybil.FlagDefs) {

	flags.PRINT_INFO = flag.Bool("info", false, "Print table info")
	flags.SORT = flag.String("sort", SORT_COUNT, "Int Column to sort by")
	flags.PRUNE_BY = flag.String("prune-sort", SORT_COUNT, "Int Column to prune intermediate results by")

	flags.LIMIT = flag.Int("limit", 100, "Number of results to return")

	flags.TIME = flag.Bool("time", false, "make a time rollup")
	flags.TIME_COL = flag.String("time-col", "time", "which column to treat as a timestamp (use with -time flag)")
	flags.TIME_BUCKET = flag.Int("time-bucket", 60*60, "time bucket (in seconds)")
	flags.WEIGHT_COL = flag.String("weight-col", "", "Which column to treat as an optional weighting column")

	flags.OP = flag.String("op", "avg", "metric to calculate, either 'avg' or 'hist'")
	flags.LOG_HIST = flag.Bool("loghist", false, "Use nested logarithmic histograms")

	flags.PRINT = flag.Bool("print", true, "Print some records")
	flags.ENCODE_RESULTS = flag.Bool("encode-results", false, "Print the results in binary format")
	flags.ENCODE_FLAGS = flag.Bool("encode-flags", false, "Print the query flags in binary format")
	flags.DECODE_FLAGS = flag.Bool("decode-flags", false, "Use the query flags supplied on stdin")
	flags.SAMPLES = flag.Bool("samples", false, "Grab samples")
	flags.INT_FILTERS = flag.String("int-filter", "", "Int filters, format: col:op:val")

	flags.HIST_BUCKET = flag.Int("int-bucket", 0, "Int hist bucket size")

	flags.STR_REPLACE = flag.String("str-replace", "", "Str replacement, format: col:find:replace")
	flags.STR_FILTERS = flag.String("str-filter", "", "Str filters, format: col:op:val")
	flags.SET_FILTERS = flag.String("set-filter", "", "Set filters, format: col:op:val")
	flags.UPDATE_TABLE_INFO = flag.Bool("update-info", false, "Re-compute cached column data")

	flags.INTS = flag.String("int", "", "Integer values to aggregate")
	flags.STRS = flag.String("str", "", "String values to load")
	flags.GROUPS = flag.String("group", "", "values group by")
	flags.DISTINCT = flag.String(sybil.DISTINCT_STR, "", "distinct group by")

	flags.EXPORT = flag.Bool("export", false, "export data to TSV")

	flags.READ_ROWSTORE = flag.Bool("read-log", false, "read the ingestion log (can take longer!)")

	flags.JSON = flag.Bool("json", false, "Print results in JSON format")
	flags.ANOVA_ICC = flag.Bool("icc", false, "Calculate intraclass co-efficient (ANOVA)")

	flags.LIST_TABLES = flag.Bool("tables", false, "List tables")

	NO_RECYCLE_MEM = flag.Bool("no-recycle-mem", false, "don't recycle memory slabs (use Go GC instead)")

	flags.CACHED_QUERIES = flag.Bool("cache-queries", false, "Cache query results per block")

}

func RunQueryCmdLine() {
	flags := sybil.DefaultFlags()
	addQueryFlags(flags)
	flag.Parse()

	if *flags.DECODE_FLAGS {
		sybil.DecodeFlags(flags)
	}

	if *flags.ENCODE_FLAGS {
		sybil.Debug("PRINTING ENCODED FLAGS")
		sybil.EncodeFlags(flags)
		return
	}

	printConfig := sybil.PrintConfig{
		Limit:         *flags.LIMIT,
		EncodeResults: *flags.ENCODE_RESULTS,
		JSON:          *flags.JSON,
	}
	if *flags.LIST_TABLES {
		sybil.PrintTables(*flags.DIR, printConfig)
		return
	}

	table := *flags.TABLE
	if table == "" {
		flag.PrintDefaults()
		return
	}

	t := sybil.GetTable(table)
	if t.IsNotExist(flags) {
		sybil.Error(t.Name, "table can not be loaded or does not exist in", *flags.DIR)
	}

	ints := make([]string, 0)
	groups := make([]string, 0)
	strs := make([]string, 0)
	distinct := make([]string, 0)

	if *flags.GROUPS != "" {
		groups = strings.Split(*flags.GROUPS, *flags.FIELD_SEPARATOR)
		sybil.OPTS.GROUP_BY = groups
	}

	if *flags.DISTINCT != "" {
		distinct = strings.Split(*flags.DISTINCT, *flags.FIELD_SEPARATOR)
		sybil.OPTS.DISTINCT = distinct
	}

	if *NO_RECYCLE_MEM {
		flags.RECYCLE_MEM = sybil.NewFalseFlag()
	}

	// PROCESS CMD LINE ARGS THAT USE COMMA DELIMITERS
	if *flags.STRS != "" {
		strs = strings.Split(*flags.STRS, *flags.FIELD_SEPARATOR)
	}
	if *flags.INTS != "" {
		ints = strings.Split(*flags.INTS, *flags.FIELD_SEPARATOR)
	}
	if *flags.PROFILE && sybil.PROFILER_ENABLED {
		profile := sybil.RUN_PROFILER()
		defer profile.Start().Stop()
	}

	if *flags.READ_ROWSTORE {
		flags.READ_INGESTION_LOG = sybil.NewTrueFlag()
	}

	// LOAD TABLE INFOS BEFORE WE CREATE OUR FILTERS, SO WE CAN CREATE FILTERS ON
	// THE RIGHT COLUMN ID
	t.LoadTableInfo(flags)
	t.LoadRecords(flags, nil)

	count := 0
	for _, block := range t.BlockList {
		count += int(block.Info.NumRecords)
	}

	sybil.Debug("WILL INSPECT", count, "RECORDS")

	groupings := []sybil.Grouping{}
	for _, g := range groups {
		groupings = append(groupings, t.Grouping(g))
	}

	aggs := []sybil.Aggregation{}
	for _, agg := range ints {
		aggs = append(aggs, t.Aggregation(flags, agg, *flags.OP))
	}

	distincts := []sybil.Grouping{}
	for _, g := range distinct {
		distincts = append(distincts, t.Grouping(g))
	}

	if *flags.OP == sybil.DISTINCT_STR {
		distincts = groupings
		groupings = make([]sybil.Grouping, 0)
	}

	// VERIFY THE KEY TABLE IS IN ORDER, OTHERWISE WE NEED TO EXIT
	sybil.Debug("KEY TABLE", t.KeyTable)
	sybil.Debug("KEY TYPES", t.KeyTypes)

	used := make(map[int16]int)
	for _, v := range t.KeyTable {
		used[v]++
		if used[v] > 1 {
			sybil.Error("THERE IS A SERIOUS KEY TABLE INCONSISTENCY")
			return
		}
	}

	loadSpec := t.NewLoadSpec()
	filterSpec := sybil.FilterSpec{Int: *flags.INT_FILTERS, Str: *flags.STR_FILTERS, Set: *flags.SET_FILTERS}
	filters := sybil.BuildFilters(flags, t, &loadSpec, filterSpec)

	queryParams := sybil.QueryParams{
		Groups:        groupings,
		Filters:       filters,
		Aggregations:  aggs,
		Distincts:     distincts,
		CachedQueries: *flags.CACHED_QUERIES,
		Samples:       *flags.SAMPLES,
	}

	querySpec := sybil.QuerySpec{QueryParams: queryParams}

	allGroups := append(groups, distinct...)
	for _, v := range allGroups {
		switch t.GetColumnType(v) {
		case sybil.STR_VAL:
			loadSpec.Str(v)
		case sybil.INT_VAL:
			loadSpec.Int(v)
		default:
			t.PrintColInfo(printConfig)
			fmt.Println("")
			sybil.Error("Unknown column type for column: ", v, t.GetColumnType(v))
		}

	}
	for _, v := range strs {
		loadSpec.Str(v)
	}
	for _, v := range ints {
		loadSpec.Int(v)
	}

	if *flags.SORT != "" {
		if *flags.SORT != sybil.OPTS.SORT_COUNT {
			loadSpec.Int(*flags.SORT)
		}
		querySpec.OrderBy = *flags.SORT
	} else {
		querySpec.OrderBy = ""
	}

	if *flags.PRUNE_BY != "" {
		if *flags.PRUNE_BY != sybil.OPTS.SORT_COUNT {
			loadSpec.Int(*flags.PRUNE_BY)
		}
		querySpec.PruneBy = *flags.PRUNE_BY
	} else {
		querySpec.PruneBy = sybil.OPTS.SORT_COUNT
	}

	if *flags.TIME {
		// TODO: infer the TimeBucket size
		querySpec.TimeBucket = *flags.TIME_BUCKET
		sybil.Debug("USING TIME BUCKET", querySpec.TimeBucket, "SECONDS")
		loadSpec.Int(*flags.TIME_COL)
		timeColID, ok := t.KeyTable[*flags.TIME_COL]
		if ok {
			sybil.OPTS.TIME_COL_ID = timeColID
		}
	}

	if *flags.WEIGHT_COL != "" {
		sybil.OPTS.WEIGHT_COL = true
		loadSpec.Int(*flags.WEIGHT_COL)
		sybil.OPTS.WEIGHT_COL_ID = t.KeyTable[*flags.WEIGHT_COL]
	}

	querySpec.Limit = *flags.LIMIT

	if *flags.SAMPLES {
		sybil.HOLD_MATCHES = true

		loadSpec := t.NewLoadSpec()
		loadSpec.LoadAllColumns = true
		loadSpec.SkipDeleteBlocksAfterQuery = true

		t.LoadAndQueryRecords(flags, &loadSpec, &querySpec)

		t.PrintSamples(sybil.PrintConfig{
			Limit:         *flags.LIMIT,
			EncodeResults: *flags.ENCODE_RESULTS,
			JSON:          *flags.JSON,
		})

		return
	}

	if *flags.EXPORT {
		loadSpec.LoadAllColumns = true
		querySpec.ExportTSV = true
	}

	if !*flags.PRINT_INFO {
		// DISABLE GC FOR QUERY PATH
		sybil.Debug("ADDING BULLET HOLES FOR SPEED (DISABLING GC)")
		debug.SetGCPercent(-1)

		sybil.Debug("USING LOAD SPEC", loadSpec)

		sybil.Debug("USING QUERY SPEC", querySpec)

		start := time.Now()
		// We can load and query at the same time
		t.LoadAndQueryRecords(flags, &loadSpec, &querySpec)

		end := time.Now()
		sybil.Debug("LOAD AND QUERY RECORDS TOOK", end.Sub(start))
		querySpec.PrintResults(*flags.OP, printConfig)

	}

	if *flags.EXPORT {
		sybil.Print("EXPORTED RECORDS TO", path.Join(t.Name, "export"))
	}

	if *flags.PRINT_INFO {
		t := sybil.GetTable(table)
		flags.LOAD_AND_QUERY = sybil.NewFalseFlag()

		t.LoadRecords(flags, nil)
		t.PrintColInfo(printConfig)
	}

}
