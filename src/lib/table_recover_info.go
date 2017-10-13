package sybil

import "fmt"
import "io/ioutil"
import "path"
import "os"
import "sync"
import "strings"

// TODO: have this only pull the blocks into column format and not materialize
// the columns immediately
func (t *Table) ReadBlockInfoFromDir(dirname string) *SavedColumnInfo {
	tb := newTableBlock()

	tb.Name = dirname

	tb.table = t

	// find out how many records are kept in this dir...
	info := SavedColumnInfo{}
	filename := fmt.Sprintf("%s/info.db", dirname)

	err := decodeInto(filename, &info)

	if err != nil {
		Warn("ERROR DECODING COLUMN BLOCK INFO!", dirname, err)
		return nil
	}

	if info.NumRecords <= 0 {
		return nil
	}

	file, _ := os.Open(dirname)
	files, _ := file.Readdir(-1)

	size := int64(0)

	var wg sync.WaitGroup
	columns := make(map[string]int)

	for _, f := range files {
		fname := f.Name()
		fsize := f.Size()
		size += fsize
		colName := fname
		colType := _NoVal

		colName = strings.TrimRight(colName, ".gz")
		colName = strings.TrimRight(colName, ".db")

		switch {
		case strings.HasPrefix(fname, "str"):
			colName = strings.Replace(colName, "str", "", 1)
			colType = StrVal
		case strings.HasPrefix(colName, "set"):
			colName = strings.Replace(colName, "set", "", 1)
			colType = SetVal
		case strings.HasPrefix(colName, "int"):
			colName = strings.Replace(colName, "int", "", 1)
			colType = IntVal

			colInfo := info.IntInfoMap[colName]
			colID := t.getKeyID(colName)
			intInfo, ok := t.IntInfo[colID]
			if !ok {
				t.IntInfo[colID] = colInfo
			} else {
				if colInfo.Min < intInfo.Min {
					intInfo.Min = colInfo.Min
				}
			}
		}

		if colName != "" {
			colID := t.getKeyID(colName)
			t.setKeyType(colID, int8(colType))
			columns[colName] = colType
		}

	}

	wg.Wait()

	return &info
}

// Alright, so... I accidentally broke my info.db file
// How can I go about loading the TableInfo based off the blocks?
// I think I go through each block and load the block, verifying the different
// column types
func (t *Table) DeduceTableInfoFromBlocks() {
	files, _ := ioutil.ReadDir(path.Join(*FLAGS.Dir, t.Name))

	var wg sync.WaitGroup
	t.initDataStructures()

	savedTable := Table{Name: t.Name}
	savedTable.initDataStructures()

	thisBlock := 0
	m := &sync.Mutex{}

	typeCounts := make(map[string]map[int]int)

	brokenMutex := sync.Mutex{}
	brokenBlocks := make([]string, 0)
	for f := range files {

		v := files[f]
		if v.IsDir() && fileLooksLikeBlock(v) {
			filename := path.Join(*FLAGS.Dir, t.Name, v.Name())
			thisBlock++

			wg.Add(1)
			go func() {
				defer wg.Done()

				info := t.ReadBlockInfoFromDir(filename)
				if info == nil {
					brokenMutex.Lock()
					brokenBlocks = append(brokenBlocks, filename)
					brokenMutex.Unlock()
					return
				}

				m.Lock()
				for col := range info.IntInfoMap {
					_, ok := typeCounts[col]
					if !ok {
						typeCounts[col] = make(map[int]int)
					}
					typeCounts[col][IntVal]++
				}
				for col := range info.StrInfoMap {
					_, ok := typeCounts[col]
					if !ok {
						typeCounts[col] = make(map[int]int)
					}
					typeCounts[col][StrVal]++
				}
				m.Unlock()

			}()
		}
	}

	wg.Wait()

	// TODO: verify that the KEY TABLE and KEY TYPES
	Debug("TYPE COUNTS", thisBlock, typeCounts)
	Debug("KEY TABLE", t.KeyTable)
	Debug("KEY TYPES", t.KeyTypes)

}
