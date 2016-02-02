package edb

import "bytes"
import "fmt"
import "time"
import "sync"
import "sync/atomic"
import "sort"

var INTERNAL_RESULT_LIMIT = 100000

type SortResultsByCol struct {
	Results []*Result

	Col string
}

func (a SortResultsByCol) Len() int      { return len(a.Results) }
func (a SortResultsByCol) Swap(i, j int) { a.Results[i], a.Results[j] = a.Results[j], a.Results[i] }

// This sorts the records in descending order
func (a SortResultsByCol) Less(i, j int) bool {
	t1 := a.Results[i].Ints[a.Col]
	t2 := a.Results[j].Ints[a.Col]

	return t1 > t2
}

func filterAndAggRecords(querySpec *QuerySpec, recordsPtr *[]*Record) []*Record {
	var buffer bytes.Buffer
	records := *recordsPtr

	ret := make([]*Record, 0)

	var result_map ResultMap;
	if querySpec.TimeBucket <= 0 {
		result_map = querySpec.Results
	}

	for i := 0; i < len(records); i++ {
		add := true
		r := records[i]

		// FILTERING
		for j := 0; j < len(querySpec.Filters); j++ {
			// returns True if the record matches!
			ret := querySpec.Filters[j].Filter(r) != true 
			if ret {
				add = false
				break
			}
		}

		if add {
			ret = append(ret, r)
		} else {
			// if we aren't adding this record... then we shouldn't continue looking at it
			continue
		}

		// BELOW HERE IS THE AGGREGATION CORE
		if len(querySpec.Groups) == 0 {
			buffer.WriteString("total")
		}

		// BUILD GROUPING KEY
		for _, g := range querySpec.Groups {
			if r.Populated[g.name_id] == 0 {
				buffer.WriteString(":")
				continue
			}
			col_id := g.name_id
			col := r.block.getColumnInfo(col_id)
			val := col.get_string_for_val(int32(r.Strs[col_id]))
			buffer.WriteString(string(val))
			buffer.WriteString(":")
		}

		// IF WE ARE DOING A TIME SERIES AGGREGATION (WHICH IS CAN BE SO MUCH SLOWER!!!!!)
		if querySpec.TimeBucket > 0 {
			val, ok := r.getIntVal("time")
			if ok {
				val = int(val / querySpec.TimeBucket) * querySpec.TimeBucket
				querySpec.r.RLock()
				result_map, ok = querySpec.TimeResults[val]
				querySpec.r.RUnlock()

				if !ok {
					result_map = make(ResultMap)
					querySpec.r.Lock()
					existing, ok := querySpec.TimeResults[val]
					if !ok {
						querySpec.TimeResults[val] = result_map
					} else {
						result_map = existing
					}
					querySpec.r.Unlock()
				}
			} else {
				continue
			}

		}

		group_key := buffer.String()
		buffer.Reset()

		querySpec.r.RLock()
		added_record, ok := result_map[group_key]
		querySpec.r.RUnlock()

		// BUILD GROUPING RECORD
		if !ok {
			length := len(result_map)

			// TODO: take into account whether we are doint time series or not...
			if length >= INTERNAL_RESULT_LIMIT {
				continue
			}

			added_record = &Result{}
			added_record.Hists = make(map[string]*Hist)
			added_record.Ints = make(map[string]float64)
			added_record.Strs = make(map[string]string)
			added_record.Sets = make(map[string][]string)
			added_record.Count = 0
			added_record.GroupByKey = group_key

			// WARNING: this is an annoying thread barrier that happens.
			// TODO: replace it with a RW mutex instead of just R mutex
			querySpec.r.RLock()
			existing_record, ok := result_map[group_key]
			querySpec.r.RUnlock()

			if !ok {
				// Even though we are about to lock, someone else might have inserted
				// right before we grabbed the lock...
				querySpec.r.Lock()
				existing_record, ok = result_map[group_key]
				if ok {
					querySpec.r.Unlock()
					added_record = existing_record
				} else { 
					result_map[group_key] = added_record
					querySpec.r.Unlock()
				}
			} else {
				added_record = existing_record
			}
		}

		count := atomic.AddInt32(&added_record.Count, 1)
		// GO THROUGH AGGREGATIONS AND REALIZE THEM

		for _, a := range querySpec.Aggregations {
			a_id := a.name_id
			if r.Populated[a_id] == INT_VAL {
				val := int(r.Ints[a_id])

				if a.op == "avg" {
					// Calculating averages
					partial, ok := added_record.Ints[a.name]
					if !ok {
						partial = 0
					}

					partial = partial + (float64(val)-partial)/float64(count)

					added_record.Ints[a.name] = partial
				}

				if a.op == "hist" {
					querySpec.m.RLock()
					hist, ok := added_record.Hists[a.name]
					querySpec.m.RUnlock()

					if !ok {
						hist = r.block.table.NewHist(r.block.table.get_int_info(a_id))
						querySpec.m.Lock()
						added_record.Hists[a.name] = hist
						querySpec.m.Unlock()
					}
					hist.addValue(val)
				}
			}

		}

	}

	return ret[:]
}

func (t *Table) MatchAndAggregate(querySpec *QuerySpec) {
	start := time.Now()

	var wg sync.WaitGroup
	rets := make([]*Record, 0)

	m := &sync.Mutex{}

	count := 0
	for _, block := range t.BlockList {
		wg.Add(1)
		this_block := block
		go func() {
			defer wg.Done()
			ret := filterAndAggRecords(querySpec, &this_block.RecordList)
			fmt.Print(".")
			count += len(ret)

			m.Lock()
			rets = append(rets, ret...)
			m.Unlock()
		}()
	}

	wg.Wait()

	fmt.Print("\n")
	end := time.Now()

	if querySpec.OrderBy != "" {
		start := time.Now()
		sorter := SortResultsByCol{}
		sorter.Results = make([]*Result, 0)
		for _, v := range querySpec.Results {
			sorter.Results = append(sorter.Results, v)
		}
		querySpec.Sorted = sorter.Results

		sorter.Col = querySpec.OrderBy
		sort.Sort(sorter)

		end := time.Now()
		if DEBUG_TIMING {
			fmt.Println("SORTING TOOK", end.Sub(start))
		}

		if len(sorter.Results) > *f_LIMIT {
			sorter.Results = sorter.Results[:*f_LIMIT]
		}

		querySpec.Sorted = sorter.Results
	}

	querySpec.Matched = rets

	fmt.Println(len(rets), "RECORDS FILTERED AND AGGREGATED INTO", len(querySpec.Results), "RESULTS, TOOK", end.Sub(start))

}