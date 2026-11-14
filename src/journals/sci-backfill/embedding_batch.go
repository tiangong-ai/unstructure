package main

import (
	"errors"
	"strings"
)

func embeddingBatch(docs []metadata, count func(string) (int, error)) ([]string, error) {
	const maxInputs = 1000
	const maxInputTokens = 8192
	const maxRequestTokens = 300000

	input := make([]string, 0, min(len(docs), maxInputs))
	total := 0
	for _, doc := range docs[:min(len(docs), maxInputs)] {
		text := strings.ReplaceAll(strings.ReplaceAll(doc.Text, "\n\n", " "), "\n", " ")
		n, err := count(text)
		if err != nil {
			return nil, err
		}
		if n < 1 || n > maxInputTokens {
			return nil, errors.New("embedding input outside model token limits")
		}
		if total+n > maxRequestTokens {
			break
		}
		input = append(input, text)
		total += n
	}
	return input, nil
}
