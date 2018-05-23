// +build go1.7

package sybil

import (
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var flagUpdateGoldenFiles = flag.Bool("update-golden", false, "update golden files")

func TestDecodeGoldenFiles(t *testing.T) {
	tests := []struct {
		name   string
		target interface{}
	}{
		{"node_results", &NodeResults{}},
		{"flag_defs", &FlagDefs{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.Open(fmt.Sprintf("testdata/TestDecodeGoldenFiles/%s.golden.gob", tt.name))
			if err != nil {
				t.Error(err)
				return
			}
			dec := gob.NewDecoder(file)
			if err = dec.Decode(tt.target); err != nil {
				t.Error(err)
			}

			asJSON, err := json.Marshal(tt.target)
			if err != nil {
				t.Error(err)
				return
			}

			goldenJSONPath := fmt.Sprintf("testdata/TestDecodeGoldenFiles/%s.golden.json", tt.name)
			if *flagUpdateGoldenFiles {
				if err := ioutil.WriteFile(goldenJSONPath, asJSON, 0644); err != nil {
					t.Error(err)
				}
				return
			}
			goldenJSON, err := ioutil.ReadFile(goldenJSONPath)
			if err != nil {
				t.Error(err)
				return
			}
			if !cmp.Equal(asJSON, goldenJSON) {
				t.Errorf("%v: golden json differs: %v", tt.name, cmp.Diff(asJSON, goldenJSON, cmp.Transformer("parsejson", func(b []byte) (m map[string]interface{}) {
					if err := json.Unmarshal(b, &m); err != nil {
						panic(err)
					}
					return m
				})))
			}
		})
	}
}