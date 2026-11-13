package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"
)

func TestPublicationRecoversOriginalReceipt(t *testing.T) {
	raw := []byte(`{"state":"SUCCESS","task_id":"sample","result":{"result":[{"text":"Hello","type":"text"}]}}`)
	path := t.TempDir() + "/paper.json.zst"
	stamp, err := publishArtifact(path, raw)
	if err != nil {
		t.Fatal(err)
	}
	actual, ok, err := publishedArtifact(path, "sample")
	if err != nil || !ok || actual.Sub(stamp).Abs() > time.Millisecond {
		t.Fatalf("unexpected published receipt: %v %t %v", actual, ok, err)
	}
	_, ok, err = publishedArtifact(path, "previous")
	if err != nil || ok {
		t.Fatal("accepted an old parser task")
	}
}

func TestCloudShardMatchesOpenSearch(t *testing.T) {
	path := os.Getenv("SCI_SHARD_FIXTURE")
	if path == "" {
		t.Skip("no isolated cloud fixture")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(os.Getenv("SCI_SEARCH_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]metadata{}
	if err = json.Unmarshal(b, &docs); err != nil {
		t.Fatal(err)
	}
	rows, err := readParquet(raw, len(docs))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		expected, found := docs[row.ID]
		var m metadata
		if !found || seen[row.ID] || json.Unmarshal([]byte(row.Metadata), &m) != nil || m != expected || !validVector(row.Values) {
			t.Fatalf("unexpected shard record %s", row.ID)
		}
		seen[row.ID] = true
	}
	t.Logf("compared %d records, 1536 dimensions, DOI IDs, and all metadata", len(rows))
}

func TestNonFiniteVectorRejected(t *testing.T) {
	values := make([]float32, 1536)
	values[0] = float32(math.Inf(1))
	if validVector(values) {
		t.Fatal("accepted infinite vector")
	}
	if _, err := decode(context.Background(), []byte("bad")); err == nil {
		t.Fatal("accepted malformed artifact")
	}
}
