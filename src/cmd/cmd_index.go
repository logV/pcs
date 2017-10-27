package cmd

import (
	"flag"
	"strings"

	. "github.com/logv/sybil/src/lib/config"
	. "github.com/logv/sybil/src/lib/structs"
	. "github.com/logv/sybil/src/query/specs"
	. "github.com/logv/sybil/src/storage/metadata_io"
	. "github.com/logv/sybil/src/query/load_and_query"
)

func RunIndexCmdLine() {
	var f_INTS = flag.String("int", "", "Integer values to index")
	flag.Parse()
	if *FLAGS.TABLE == "" {
		flag.PrintDefaults()
		return
	}

	var ints []string
	if *f_INTS != "" {
		ints = strings.Split(*f_INTS, *FLAGS.FIELD_SEPARATOR)
	}

	FLAGS.UPDATE_TABLE_INFO = &TRUE

	t := GetTable(*FLAGS.TABLE)

	LoadRecords(t, nil)
	SaveTableInfo(t, "info")
	OPTS.DELETE_BLOCKS_AFTER_QUERY = true
	OPTS.WRITE_BLOCK_INFO = true

	loadSpec := NewTableLoadSpec(t)
	for _, v := range ints {
		loadSpec.Int(v)
	}
	LoadRecords(t, &loadSpec)
	SaveTableInfo(t, "info")
}
