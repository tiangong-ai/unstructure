package artifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

type Response struct {
	TaskID string `json:"task_id"`
	State  string `json:"state"`
	Result struct {
		Chunks []json.RawMessage `json:"result"`
	} `json:"result"`
}

func Parse(raw []byte, expectedTaskID string) (Response, error) {
	var response Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return response, fmt.Errorf("decode MinerU response: %w", err)
	}
	if response.State != "SUCCESS" || response.TaskID == "" || len(response.Result.Chunks) == 0 ||
		(expectedTaskID != "" && response.TaskID != expectedTaskID) {
		return response, errors.New("MinerU response does not match the successful task")
	}
	typed := 0
	for _, chunk := range response.Result.Chunks {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(chunk, &fields); err != nil {
			return response, errors.New("invalid MinerU chunk")
		}
		var text string
		if len(fields["text"]) == 0 || bytes.Equal(fields["text"], []byte("null")) || json.Unmarshal(fields["text"], &text) != nil {
			return response, errors.New("MinerU chunk is missing text")
		}
		if value := fields["type"]; len(value) != 0 && !bytes.Equal(value, []byte("null")) {
			typed++
		}
	}
	if typed == 0 {
		return response, errors.New("MinerU returned no typed blocks")
	}
	return response, nil
}

func Compress(raw []byte) ([]byte, error) {
	writer, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(9)))
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	return writer.EncodeAll(raw, nil), nil
}

func Decompress(compressed []byte) ([]byte, error) {
	reader, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(128<<20))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return reader.DecodeAll(compressed, nil)
}
