package sybil

import (
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/logv/sybil/src/lib/common"
)

func TestTableDigestRowRecords(test *testing.T) {
	deleteTestDB()

	blockCount := 3
	addRecordsToTestDB(func(r *Record, index int) {
		r.AddIntField("id", int64(index))
		age := int64(rand.Intn(20)) + 10
		r.AddIntField("age", age)
		r.AddStrField("ageStr", strconv.FormatInt(int64(age), 10))
	}, blockCount)

	t := GetTable(testTableName)
	t.IngestRecords("ingest")

	unloadTestTable()
	nt := GetTable(testTableName)

	DELETE_BLOCKS_AFTER_QUERY = false
	common.FLAGS.READ_INGESTION_LOG = &common.TRUE

	nt.LoadTableInfo()
	nt.LoadRecords(nil)

	if len(nt.RowBlock.RecordList) != CHUNK_SIZE*blockCount {
		test.Error("Row Store didn't read back right number of records", len(nt.RowBlock.RecordList))
	}

	if len(nt.BlockList) != 1 {
		test.Error("Found other records than rowblock")
	}

	nt.DigestRecords()

	unloadTestTable()

	READ_ROWS_ONLY = false
	nt = GetTable(testTableName)
	nt.LoadRecords(nil)

	count := int32(0)
	for _, b := range nt.BlockList {
		common.Debug("COUNTING RECORDS IN", b.Name)
		count += b.Info.NumRecords
	}

	if count != int32(blockCount*CHUNK_SIZE) {
		test.Error("COLUMN STORE RETURNED TOO FEW COLUMNS", count)
	}

}

func TestColumnStoreFileNames(test *testing.T) {

	deleteTestDB()

	blockCount := 3
	addRecordsToTestDB(func(r *Record, index int) {
		r.AddIntField("id", int64(index))
		age := int64(rand.Intn(20)) + 10
		r.AddIntField("age", age)
		r.AddStrField("ageStr", strconv.FormatInt(int64(age), 10))
		r.AddSetField("ageSet", []string{strconv.FormatInt(int64(age), 10)})
	}, blockCount)

	t := GetTable(testTableName)
	t.IngestRecords("ingest")

	unloadTestTable()
	nt := GetTable(testTableName)
	DELETE_BLOCKS_AFTER_QUERY = false
	FLAGS.READ_INGESTION_LOG = &TRUE

	nt.LoadTableInfo()
	nt.LoadRecords(nil)

	if len(nt.RowBlock.RecordList) != CHUNK_SIZE*blockCount {
		test.Error("Row Store didn't read back right number of records", len(nt.RowBlock.RecordList))
	}

	if len(nt.BlockList) != 1 {
		test.Error("Found other records than rowblock")
	}

	nt.DigestRecords()

	unloadTestTable()

	READ_ROWS_ONLY = false
	nt = GetTable(testTableName)
	nt.LoadRecords(nil)

	count := int32(0)

	for _, b := range nt.BlockList {
		Debug("COUNTING RECORDS IN", b.Name)
		count += b.Info.NumRecords

		file, _ := os.Open(b.Name)
		files, _ := file.Readdir(-1)
		created_files := make(map[string]bool)

		for _, f := range files {
			created_files[f.Name()] = true
		}

		Debug("FILENAMES", created_files)
		Debug("BLOCK NAME", b.Name)
		if b.Name == ROW_STORE_BLOCK {
			continue
		}

		var colFiles = []string{"int_age.db", "int_id.db", "str_ageStr.db", "set_ageSet.db"}
		for _, filename := range colFiles {
			_, ok := created_files[filename]
			if !ok {
				test.Error("MISSING COLUMN FILE", filename)
			}

		}

	}

	if count != int32(blockCount*CHUNK_SIZE) {
		test.Error("COLUMN STORE RETURNED TOO FEW COLUMNS", count)
	}

}

func TestBigIntColumns(test *testing.T) {
	deleteTestDB()

	var minVal = int64(1 << 50)
	blockCount := 3
	addRecordsToTestDB(func(r *Record, index int) {
		r.AddIntField("id", int64(index))
		age := int64(rand.Intn(1 << 20))
		r.AddIntField("time", minVal+age)
	}, blockCount)

	t := GetTable(testTableName)
	t.IngestRecords("ingest")

	unloadTestTable()
	nt := GetTable(testTableName)
	DELETE_BLOCKS_AFTER_QUERY = false
	FLAGS.READ_INGESTION_LOG = &TRUE

	nt.LoadTableInfo()
	nt.LoadRecords(nil)

	if len(nt.RowBlock.RecordList) != CHUNK_SIZE*blockCount {
		test.Error("Row Store didn't read back right number of records", len(nt.RowBlock.RecordList))
	}

	if len(nt.BlockList) != 1 {
		test.Error("Found other records than rowblock")
	}

	nt.DigestRecords()

	unloadTestTable()

	READ_ROWS_ONLY = false
	FLAGS.SAMPLES = &TRUE
	limit := 1000
	FLAGS.LIMIT = &limit
	nt = GetTable(testTableName)

	loadSpec := nt.NewLoadSpec()
	loadSpec.LoadAllColumns = true
	nt.LoadRecords(&loadSpec)

	count := int32(0)
	Debug("MIN VALUE BEING CHECKED FOR IS", minVal, "2^32 is", 1<<32)
	Debug("MIN VAL IS BIGGER?", minVal > 1<<32)
	for _, b := range nt.BlockList {
		Debug("VERIFYING BIG INTS IN", b.Name)
		for _, r := range b.RecordList {
			v, ok := r.GetIntVal("time")
			if int64(v) < minVal || !ok {
				test.Error("BIG INT UNPACKED INCORRECTLY! VAL:", v, "OK?", ok)
			}

		}
		count += b.Info.NumRecords
	}

	if count != int32(blockCount*CHUNK_SIZE) {
		test.Error("COLUMN STORE RETURNED TOO FEW COLUMNS", count)

	}
	FLAGS.SAMPLES = &FALSE

}
