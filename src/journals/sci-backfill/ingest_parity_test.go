package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

func TestEmbeddingBatchLimits(t *testing.T) {
	docs := make([]metadata, 1001)
	for i := range docs {
		docs[i].Text = "first\n\nsecond\nthird"
	}
	for _, tc := range []struct{ tokens, want int }{{1, 1000}, {8000, 37}} {
		got, err := embeddingBatch(docs, func(s string) (int, error) {
			if s != "first second third" {
				t.Fatalf("embedding text changed: %q", s)
			}
			return tc.tokens, nil
		})
		if err != nil || len(got) != tc.want {
			t.Fatalf("%d tokens per input: got %d inputs, want %d: %v", tc.tokens, len(got), tc.want, err)
		}
	}
}

func TestPythonLongTables(t *testing.T) {
	data, err := os.ReadFile("testdata/python-long-tables.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Input    string
		Expected []string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range fixtures {
		got, err := splitLongTables(f.Input, codec.Count, 8100)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, f.Expected) {
			lengths := func(parts []string) []int {
				var out []int
				for _, p := range parts {
					out = append(out, strings.Count(p, "<tr>"))
				}
				return out
			}
			t.Errorf("fixture %d: rows per part Go=%v Python=%v", i, lengths(got), lengths(f.Expected))
			if len(got) > 0 && len(f.Expected) > 0 {
				t.Logf("first part Go %.240q, Python %.240q", got[0], f.Expected[0])
			}
		}
	}
}
