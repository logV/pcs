package sybil

import (
	"os"
	"path"
	"strings"
	"time"

	. "github.com/logv/sybil/src/lib/common"
	"github.com/logv/sybil/src/lib/config"
	. "github.com/logv/sybil/src/lib/encoders"
	. "github.com/logv/sybil/src/lib/locks"
	. "github.com/logv/sybil/src/lib/metadata"
	. "github.com/logv/sybil/src/lib/record"
	. "github.com/logv/sybil/src/lib/structs"
	. "github.com/logv/sybil/src/lib/table_column"
)

type RowSavedInt struct {
	Name  int16
	Value int64
}

type RowSavedStr struct {
	Name  int16
	Value string
}

type RowSavedSet struct {
	Name  int16
	Value []string
}

type SavedRecord struct {
	Ints []RowSavedInt
	Strs []RowSavedStr
	Sets []RowSavedSet
}

type AfterRowBlockLoad func(string, RecordList)

func LoadRowStoreRecords(t *Table, digest string, after_block_load_cb AfterRowBlockLoad) {
	dirname := path.Join(*config.FLAGS.DIR, t.Name, digest)
	var err error

	// if the row store dir does not exist, skip the whole function
	_, err = os.Stat(dirname)
	if os.IsNotExist(err) {
		if after_block_load_cb != nil {
			after_block_load_cb(NO_MORE_BLOCKS, nil)
		}

		return
	}

	var file *os.File
	for i := 0; i < LOCK_TRIES; i++ {
		file, err = os.Open(dirname)
		if err != nil {
			Debug("Can't open the ingestion dir", dirname)
			time.Sleep(LOCK_US)
			if i > MAX_ROW_STORE_TRIES {
				return
			}
			continue
		}
		break
	}

	files, err := file.Readdir(0)
	if t.RowBlock == nil {
		t.RowBlock = &TableBlock{}
		(*t.RowBlock).RecordList = make(RecordList, 0)
		t.RowBlock.Info = &SavedColumnInfo{}
		t.BlockMutex.Lock()
		t.BlockList[ROW_STORE_BLOCK] = t.RowBlock
		t.BlockMutex.Unlock()
		t.RowBlock.Name = ROW_STORE_BLOCK
	}

	for _, file := range files {
		filename := file.Name()

		// we can open .gz files as well as regular .db files
		cname := strings.TrimRight(filename, GZIP_EXT)

		if strings.HasSuffix(cname, ".db") == false {
			continue
		}

		filename = path.Join(dirname, file.Name())

		records := LoadRecordsFromLog(t, filename)
		if after_block_load_cb != nil {
			after_block_load_cb(filename, records)
		}
	}

	if after_block_load_cb != nil {
		after_block_load_cb(NO_MORE_BLOCKS, nil)
	}

}

func LoadRecordsFromLog(t *Table, filename string) RecordList {
	var marshalled_records []*SavedRecord

	// Create an encoder and send a value.
	err := DecodeInto(filename, &marshalled_records)
	if err != nil {
		Debug("ERROR LOADING INGESTION LOG", err)
	}

	ret := make(RecordList, len(marshalled_records))

	for i, r := range marshalled_records {
		ret[i] = r.toRecord(t)
	}
	return ret

}
func (s SavedRecord) toRecord(t *Table) *Record {
	r := Record{}
	r.Ints = IntArr{}
	r.Strs = StrArr{}
	r.SetMap = SetMap{}

	b := t.LastBlock
	t.LastBlock.RecordList = append(t.LastBlock.RecordList, &r)

	b.Table = t
	r.Block = &b

	max_key_id := 0
	for _, v := range t.KeyTable {
		if max_key_id <= int(v) {
			max_key_id = int(v) + 1
		}
	}

	ResizeFields(&r, int16(max_key_id))

	for _, v := range s.Ints {
		r.Populated[v.Name] = INT_VAL
		r.Ints[v.Name] = IntField(v.Value)
		UpdateTableIntInfo(t, v.Name, v.Value)
	}

	for _, v := range s.Strs {
		AddStrField(&r, GetTableStringForKey(t, int(v.Name)), v.Value)
	}

	for _, v := range s.Sets {
		AddSetField(&r, GetTableStringForKey(t, int(v.Name)), v.Value)
		r.Populated[v.Name] = SET_VAL
	}

	return &r
}

func ToSavedRecord(r *Record) *SavedRecord {
	s := SavedRecord{}
	for k, v := range r.Ints {
		if r.Populated[k] == INT_VAL {
			s.Ints = append(s.Ints, RowSavedInt{int16(k), int64(v)})
		}
	}

	for k, v := range r.Strs {
		if r.Populated[k] == STR_VAL {
			col := GetColumnInfo(r.Block, int16(k))
			str_val := GetColumnStringForVal(col, int32(v))
			s.Strs = append(s.Strs, RowSavedStr{int16(k), str_val})
		}
	}

	for k, v := range r.SetMap {
		if r.Populated[k] == SET_VAL {
			col := GetColumnInfo(r.Block, int16(k))
			set_vals := make([]string, len(v))
			for i, val := range v {
				set_vals[i] = GetColumnStringForVal(col, int32(val))
			}
			s.Sets = append(s.Sets, RowSavedSet{int16(k), set_vals})
		}
	}

	return &s

}

type SavedRecords struct {
	RecordList []*SavedRecord
}
