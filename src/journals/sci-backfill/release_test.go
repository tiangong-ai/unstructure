package main

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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

func TestHistoricalPDFPath(t *testing.T) {
	for _, tc := range []struct{ path, doi string }{
		{"10.1002/we.2016.pdf", "10.1002/we.2016"},
		{"10.1016/0006-3207%252869%252990181-5.pdf", "10.1016/0006-3207(69)90181-5"},
		{"10.1002/sici%252Bfoo%253Abar%253C1%253E.pdf", "10.1002/sici+foo:bar<1>"},
	} {
		got, err := doiFromPDFPath(tc.path)
		if err != nil || got != tc.doi {
			t.Errorf("decoded %q as %q: %v", tc.path, got, err)
		}
		if got := encodedPDFPath(tc.doi); got != tc.path {
			t.Errorf("encoded %q as %q, want %q", tc.doi, got, tc.path)
		}
	}
	for _, candidate := range []string{
		"/10.1002/we.2016.pdf",
		"10.1016/0006-3207%2869%2990181-5.pdf",
		"10.1002/%252e%252e/other.pdf",
		"10.1002/we.2016.txt",
	} {
		if _, err := doiFromPDFPath(candidate); err == nil {
			t.Errorf("unexpectedly accepted %q", candidate)
		}
	}
}

func TestMinerUSubmitReadsOriginalEncodedFile(t *testing.T) {
	const rel = "10.1016/0006-3207%252869%252990181-5.pdf"
	const pdf = "%PDF-1.7\nencoded fixture\n"
	const id = "092ee3c1-e1e4-4ef1-9f9f-fc7642f9de94"
	root := t.TempDir()
	filename := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(pdf), 0600); err != nil {
		t.Fatal(err)
	}
	doi, err := doiFromPDFPath(rel)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		if string(body) != pdf || err != nil || header.Filename != filepath.Base(rel) {
			t.Errorf("wrong PDF uploaded: %q, %q, %v", header.Filename, body, err)
		}
		io.WriteString(w, `{"task_id":"`+id+`"}`)
	}))
	defer server.Close()
	got, err := mineruSubmit(context.Background(), server.Client(), server.URL, "fixture", root, parseTask{id: id, doi: doi, attempt: 1})
	if err != nil || got != id {
		t.Fatalf("submission returned %q: %v", got, err)
	}
}

type importListing []types.Object

func (l importListing) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &s3.ListObjectsV2Output{Contents: l}, nil
}

func TestImportDirectoryRetainsEarlierShard(t *testing.T) {
	prefix := "processed_docs/pinecone/sci_v1/"
	first := "processed_docs/pinecone/sci_v1/part-sci-historical-v1-000000000001.parquet"
	second := shardKey(prefix, "sci-historical-v2", 2)
	if shardKey(prefix, "sci-historical-v1", 1) != first {
		t.Fatal("key for the published first shard changed")
	}
	expected := map[string]int64{first: 14, second: 28}
	older := types.Object{Key: aws.String(first), Size: aws.Int64(14)}
	newer := types.Object{Key: aws.String(second), Size: aws.Int64(28)}
	extra := types.Object{Key: aws.String(prefix + "unregistered.parquet"), Size: aws.Int64(10)}
	wrongSize := types.Object{Key: aws.String(first), Size: aws.Int64(15)}
	for _, tc := range []struct {
		name      string
		list      importListing
		wantError bool
	}{
		{"both batches", importListing{older, newer}, false},
		{"missing earlier shard", importListing{newer}, true},
		{"unregistered object", importListing{older, newer, extra}, true},
		{"changed earlier shard size", importListing{wrongSize, newer}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkImportObjects(context.Background(), tc.list, prefix, maps.Clone(expected))
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected inventory result: %v", err)
			}
		})
	}
}
