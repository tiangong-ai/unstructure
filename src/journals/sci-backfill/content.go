package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"local/sci-ingest-pilot/internal/artifact"
	"local/sci-ingest-pilot/internal/r9"
	"log"
	"net/http"
	"strconv"
	"strings"
)

type parserResponse = artifact.Response

type metadata struct {
	Text    string `json:"text"`
	DOI     string `json:"doi"`
	Journal string `json:"journal"`
	Date    int64  `json:"date"`
}

func decode(ctx context.Context, encoded []byte) (parserResponse, error) {
	if err := ctx.Err(); err != nil {
		return parserResponse{}, err
	}
	raw, err := artifact.Decompress(encoded)
	if err != nil {
		return parserResponse{}, fmt.Errorf("decompress parser response: %w", err)
	}
	return artifact.Parse(raw, "")
}

func (r *runner) clean(ctx context.Context, response parserResponse) ([]string, error) {
	blocks := make([]r9.Block, len(response.Result.Chunks))
	for i, raw := range response.Result.Chunks {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&blocks[i]); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := r9.Clean(blocks)
	if err != nil {
		return nil, fmt.Errorf("R9 clean: %w", err)
	}
	lines := make([]string, len(result.Cleaned))
	for i, block := range result.Cleaned {
		lines[i] = block["text"].(string)
	}
	var chunks []string
	pending := ""
	for _, line := range lines {
		n, err := r.codec.Count(line)
		if err != nil {
			return nil, err
		}
		if n > 8100 {
			tables, err := splitLongTables(line, r.codec.Count, 8100)
			if err != nil {
				return nil, err
			}
			chunks = append(chunks, tables...)
			continue
		}
		if n < 15 {
			pending += line + " "
		} else {
			chunks = append(chunks, pending+line)
			pending = ""
		}
	}
	if strings.TrimSpace(pending) != "" {
		chunks = append(chunks, pending)
	}
	if len(chunks) == 0 {
		return nil, errors.New("empty final chunks")
	}
	for i := range chunks {
		chunks[i] = strings.ReplaceAll(chunks[i], "\uFFFD", " ")
		n, err := r.codec.Count(chunks[i])
		if err != nil || n > 8192 || strings.TrimSpace(chunks[i]) == "" {
			return nil, errors.New("invalid merged chunk")
		}
	}
	return chunks, nil
}

func (r *runner) vector(ctx context.Context, task, doi string, docs []metadata) ([]parquetRow, error) {
	type vector struct {
		ID       string    `json:"id"`
		Values   []float32 `json:"values"`
		Metadata metadata  `json:"metadata"`
	}
	var vectors []vector
	log.Printf("embedding start task=%s chunks=%d", task, len(docs))
	for start := 0; start < len(docs); {
		input, err := embeddingBatch(docs[start:], r.codec.Count)
		if err != nil {
			return nil, err
		}
		var embeddings struct {
			Model string
			Data  []struct {
				Index     int
				Embedding []float32
			}
		}
		req := map[string]any{"model": "text-embedding-3-small", "input": input}
		if err := r.request(ctx, http.MethodPost, "https://api.openai.com/v1/embeddings", req, "Authorization", "Bearer "+r.cfg.OpenAIKey, &embeddings); err != nil {
			return nil, err
		}
		if embeddings.Model != "text-embedding-3-small" || len(embeddings.Data) != len(input) {
			return nil, errors.New("unexpected embedding model or row count")
		}
		for i, record := range embeddings.Data {
			if record.Index != i || len(record.Embedding) != 1536 {
				return nil, errors.New("invalid embedding index or dimension")
			}
			j := start + i
			vectors = append(vectors, vector{ID: chunkID(doi, j), Values: record.Embedding, Metadata: docs[j]})
		}
		start += len(input)
	}
	rows := make([]parquetRow, 0, len(vectors))
	for _, vector := range vectors {
		encoded, err := json.Marshal(vector.Metadata)
		if err != nil {
			return nil, err
		}
		rows = append(rows, parquetRow{ID: vector.ID, Values: vector.Values, Metadata: string(encoded)})
	}
	return rows, nil
}

func chunkID(doi string, i int) string {
	return doi + "_" + strconv.Itoa(i)
}
