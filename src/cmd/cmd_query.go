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

func addPrintFlags() {
	flag.StringVar(&sybil.FLAGS.OP, "op", "avg", "metric to calculate, either 'avg' or 'hist'")
	flag.BoolVar(&sybil.FLAGS.LIST_TABLES, "tables", false, "List tables")
	flag.BoolVar(&sybil.FLAGS.PRINT_INFO, "info", false, "Print table info")
	flag.IntVar(&sybil.FLAGS.LIMIT, "limit", 100, "Number of results to return")
	flag.BoolVar(&sybil.FLAGS.PRINT, "print", true, "Print some records")
	flag.BoolVar(&sybil.FLAGS.SAMPLES, "samples", false, "Grab samples")
	flag.BoolVar(&sybil.FLAGS.JSON, "json", false, "Print results in JSON format")
}

func addQueryFlags() {

	flag.StringVar(&sybil.FLAGS.SORT, "sort", SORT_COUNT, "Int Column to sort by")
	flag.StringVar(&sybil.FLAGS.PRUNE_BY, "prune-sort", SORT_COUNT, "Int Column to prune intermediate results by")

	flag.BoolVar(&sybil.FLAGS.TIME, "time", false, "make a time rollup")
	flag.StringVar(&sybil.FLAGS.TIME_COL, "time-col", "time", "which column to treat as a timestamp (use with -time flag)")
	flag.IntVar(&sybil.FLAGS.TIME_BUCKET, "time-bucket", 60*60, "time bucket (in seconds)")
	flag.StringVar(&sybil.FLAGS.WEIGHT_COL, "weight-col", "", "Which column to treat as an optional weighting column")

	flag.BoolVar(&sybil.FLAGS.LOG_HIST, "loghist", false, "Use nested logarithmic histograms")

	flag.BoolVar(&sybil.FLAGS.ENCODE_RESULTS, "encode-results", false, "Print the results in binary format")
	flag.BoolVar(&sybil.FLAGS.ENCODE_FLAGS, "encode-flags", false, "Print the query flags in binary format")
	flag.BoolVar(&sybil.FLAGS.DECODE_FLAGS, "decode-flags", false, "Use the query flags supplied on stdin")
	flag.StringVar(&sybil.FLAGS.INT_FILTERS, "int-filter", "", "Int filters, format: col:op:val")

	flag.IntVar(&sybil.FLAGS.HIST_BUCKET, "int-bucket", 0, "Int hist bucket size")

	flag.StringVar(&sybil.FLAGS.STR_REPLACE, "str-replace", "", "Str replacement, format: col:find:replace")
	flag.StringVar(&sybil.FLAGS.STR_FILTERS, "str-filter", "", "Str filters, format: col:op:val")
	flag.StringVar(&sybil.FLAGS.SET_FILTERS, "set-filter", "", "Set filters, format: col:op:val")
	flag.BoolVar(&sybil.FLAGS.UPDATE_TABLE_INFO, "update-info", false, "Re-compute cached column data")

	flag.StringVar(&sybil.FLAGS.INTS, "int", "", "Integer values to aggregate")
	flag.StringVar(&sybil.FLAGS.STRS, "str", "", "String values to load")
	flag.StringVar(&sybil.FLAGS.GROUPS, "group", "", "values group by")
	flag.StringVar(&sybil.FLAGS.DISTINCT, sybil.OP_DISTINCT, "", "distinct group by")

	flag.BoolVar(&sybil.FLAGS.EXPORT, "export", false, "export data to TSV")

	flag.BoolVar(&sybil.FLAGS.READ_ROWSTORE, "read-log", false, "read the ingestion log (can take longer!)")

	flag.BoolVar(&sybil.FLAGS.ANOVA_ICC, "icc", false, "Calculate intraclass co-efficient (ANOVA)")

	f := false
	NO_RECYCLE_MEM = &f
	flag.BoolVar(NO_RECYCLE_MEM, "no-recycle-mem", false, "don't recycle memory slabs (use Go GC instead)")

	flag.BoolVar(&sybil.FLAGS.CACHED_QUERIES, "cache-queries", false, "Cache query results per block")

}

func RunQueryCmdLine() {
	addQueryFlags()
	addPrintFlags()
	flag.Parse()

	if sybil.FLAGS.DECODE_FLAGS {
		sybil.DecodeFlags()
	}

	if sybil.FLAGS.ENCODE_FLAGS {
		sybil.Debug("PRINTING ENCODED FLAGS")
		sybil.EncodeFlags()
		return
	}

	printSpec := &sybil.PrintSpec{
		ListTables: sybil.FLAGS.LIST_TABLES,
		PrintInfo:  sybil.FLAGS.PRINT_INFO,
		Samples:    sybil.FLAGS.SAMPLES,

		Op:            sybil.Op(sybil.FLAGS.OP),
		Limit:         sybil.FLAGS.LIMIT,
		EncodeResults: sybil.FLAGS.ENCODE_RESULTS,
		JSON:          sybil.FLAGS.JSON,
	}
	if sybil.FLAGS.LIST_TABLES {
		sybil.PrintTables(printSpec)
		return
	}

	table := sybil.FLAGS.TABLE
	if table == "" {
		flag.PrintDefaults()
		return
	}

	t := sybil.GetTable(table)
	if t.IsNotExist() {
		sybil.Error(t.Name, "table can not be loaded or does not exist in", sybil.FLAGS.DIR)
	}

	ints := make([]string, 0)
	groups := make([]string, 0)
	strs := make([]string, 0)
	distinct := make([]string, 0)

	if sybil.FLAGS.GROUPS != "" {
		groups = strings.Split(sybil.FLAGS.GROUPS, sybil.FLAGS.FIELD_SEPARATOR)
	}

	if sybil.FLAGS.DISTINCT != "" {
		distinct = strings.Split(sybil.FLAGS.DISTINCT, sybil.FLAGS.FIELD_SEPARATOR)
	}

	if *NO_RECYCLE_MEM {
		sybil.FLAGS.RECYCLE_MEM = false
	}

	// PROCESS CMD LINE ARGS THAT USE COMMA DELIMITERS
	if sybil.FLAGS.STRS != "" {
		strs = strings.Split(sybil.FLAGS.STRS, sybil.FLAGS.FIELD_SEPARATOR)
	}
	if sybil.FLAGS.INTS != "" {
		ints = strings.Split(sybil.FLAGS.INTS, sybil.FLAGS.FIELD_SEPARATOR)
	}
	if sybil.FLAGS.PROFILE && sybil.PROFILER_ENABLED {
		profile := sybil.RUN_PROFILER()
		defer profile.Start().Stop()
	}

	if sybil.FLAGS.READ_ROWSTORE {
		sybil.FLAGS.READ_INGESTION_LOG = true
	}

	// LOAD TABLE INFOS BEFORE WE CREATE OUR FILTERS, SO WE CAN CREATE FILTERS ON
	// THE RIGHT COLUMN ID
	t.LoadTableInfo()
	t.LoadRecords(nil)

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
	var op sybil.Op
	switch sybil.Op(sybil.FLAGS.OP) {
	case sybil.OP_HIST:
		op = sybil.OP_HIST
	case sybil.OP_AVG:
		op = sybil.OP_AVG
	case sybil.OP_DISTINCT:
		op = sybil.OP_DISTINCT
	}
	for _, agg := range ints {
		aggs = append(aggs, t.Aggregation(agg, op))
	}

	distincts := []sybil.Grouping{}
	for _, g := range distinct {
		distincts = append(distincts, t.Grouping(g))
	}

	if op == sybil.OP_DISTINCT {
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
	filterSpec := sybil.FilterSpec{Int: sybil.FLAGS.INT_FILTERS, Str: sybil.FLAGS.STR_FILTERS, Set: sybil.FLAGS.SET_FILTERS}
	filters := sybil.BuildFilters(t, &loadSpec, filterSpec)

	replacements := sybil.BuildReplacements(sybil.FLAGS.FIELD_SEPARATOR, sybil.FLAGS.STR_REPLACE)
	queryParams := sybil.QueryParams{
		Groups:       groupings,
		Filters:      filters,
		Aggregations: aggs,
		Distincts:    distincts,

		CachedQueries: sybil.FLAGS.CACHED_QUERIES,
		StrReplace:    replacements,
	}
	if op == sybil.OP_HIST {
		histType := sybil.HistogramTypeBasic
		if sybil.FLAGS.LOG_HIST {
			histType = sybil.HistogramTypeLog
		}
		queryParams.HistogramParameters = sybil.HistogramParameters{
			Type:       histType,
			BucketSize: sybil.FLAGS.HIST_BUCKET,
			Weighted:   sybil.FLAGS.WEIGHT_COL != "",
		}
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
			t.PrintColInfo(printSpec)
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

	if sybil.FLAGS.SORT != "" {
		if sybil.FLAGS.SORT != sybil.SORT_COUNT {
			loadSpec.Int(sybil.FLAGS.SORT)
		}
		querySpec.OrderBy = sybil.FLAGS.SORT
	} else {
		querySpec.OrderBy = ""
	}

	if sybil.FLAGS.PRUNE_BY != "" {
		if sybil.FLAGS.PRUNE_BY != sybil.SORT_COUNT {
			loadSpec.Int(sybil.FLAGS.PRUNE_BY)
		}
		querySpec.PruneBy = sybil.FLAGS.PRUNE_BY
	} else {
		querySpec.PruneBy = sybil.SORT_COUNT
	}

	if sybil.FLAGS.TIME {
		// TODO: infer the TimeBucket size
		querySpec.TimeBucket = sybil.FLAGS.TIME_BUCKET
		sybil.Debug("USING TIME BUCKET", querySpec.TimeBucket, "SECONDS")
		loadSpec.Int(sybil.FLAGS.TIME_COL)
	}

	if sybil.FLAGS.WEIGHT_COL != "" {
		loadSpec.Int(sybil.FLAGS.WEIGHT_COL)
	}

	querySpec.Limit = sybil.FLAGS.LIMIT

	if sybil.FLAGS.SAMPLES {
		sybil.HOLD_MATCHES = true

		loadSpec := t.NewLoadSpec()
		loadSpec.LoadAllColumns = true
		loadSpec.SkipDeleteBlocksAfterQuery = true
		querySpec.Samples = true

		t.LoadAndQueryRecords(&loadSpec, &querySpec)

		t.PrintSamples(printSpec)

		return
	}

	if sybil.FLAGS.EXPORT {
		loadSpec.LoadAllColumns = true
	}

	if !sybil.FLAGS.PRINT_INFO {
		// DISABLE GC FOR QUERY PATH
		sybil.Debug("ADDING BULLET HOLES FOR SPEED (DISABLING GC)")
		debug.SetGCPercent(-1)

		sybil.Debug("USING LOAD SPEC", loadSpec)

		sybil.Debug("USING QUERY SPEC", querySpec)

		start := time.Now()

		if sybil.FLAGS.LOAD_AND_QUERY {
			t.LoadAndQueryRecords(&loadSpec, &querySpec)

			end := time.Now()
			sybil.Debug("LOAD AND QUERY RECORDS TOOK", end.Sub(start))
			querySpec.PrintResults(printSpec)
		}

	}

	if sybil.FLAGS.EXPORT {
		sybil.Print("EXPORTED RECORDS TO", path.Join(t.Name, "export"))
	}

	if sybil.FLAGS.PRINT_INFO {
		t := sybil.GetTable(table)
		sybil.FLAGS.LOAD_AND_QUERY = false

		t.LoadRecords(nil)
		t.PrintColInfo(printSpec)
	}

}
