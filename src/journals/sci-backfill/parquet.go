package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	parquet "github.com/parquet-go/parquet-go"
)

type parquetRow struct {
	ID       string    `parquet:"id"`
	Values   []float32 `parquet:"values,list"`
	Metadata string    `parquet:"metadata"`
}

func s3Status(err error, code int) bool {
	var response *smithyhttp.ResponseError
	return errors.As(err, &response) && response.HTTPStatusCode() == code
}

const maxStage = 64 << 20

func sha(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (r *runner) putImmutable(ctx context.Context, key string, data []byte) error {
	_, err := r.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(data),
		ExpectedBucketOwner: aws.String(bucketOwner), IfNoneMatch: aws.String("*"), ContentType: aws.String("application/vnd.apache.parquet"),
		Metadata: map[string]string{"sha256": sha(data)}})
	return err
}

func readParquet(data []byte, expected int) ([]parquetRow, error) {
	file, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if file.NumRows() != int64(expected) || !parquet.EqualNodes(file.Schema(), parquet.SchemaOf(parquetRow{})) {
		return nil, errors.New("Parquet shape mismatch")
	}
	return parquet.Read[parquetRow](bytes.NewReader(data), int64(len(data)))
}

func (r *runner) ensureStage(ctx context.Context, t task, docs []metadata) (int, string, error) {
	key := r.cfg.stagingPrefix() + t.id + ".parquet"
	verify := func(data []byte, attrs map[string]string) (int, string, error) {
		digest := sha(data)
		if attrs["sha256"] != digest {
			return 0, "", errors.New("staging checksum mismatch")
		}
		rows, err := readParquet(data, len(docs))
		if err != nil {
			return 0, "", err
		}
		for i, row := range rows {
			var m metadata
			if row.ID != chunkID(t.doi, i) || json.Unmarshal([]byte(row.Metadata), &m) != nil || m != docs[i] || !validVector(row.Values) {
				return 0, "", fmt.Errorf("stage row %d does not match task", i)
			}
		}
		log.Printf("reused uploaded Parquet task=%s key=%s", t.id, key)
		return len(data), digest, nil
	}
	if t.attempt > 1 {
		data, attrs, err := r.get(ctx, key, maxStage)
		if err == nil {
			return verify(data, attrs)
		}
		if !s3Status(err, 404) {
			return 0, "", err
		}
	}
	rows, err := r.vector(ctx, t.id, t.doi, docs)
	if err != nil {
		return 0, "", err
	}
	var out bytes.Buffer
	for _, row := range rows {
		if !validVector(row.Values) {
			return 0, "", errors.New("invalid embedding vector")
		}
	}
	if err := parquet.Write(&out, rows); err != nil {
		return 0, "", err
	}
	data := out.Bytes()
	if len(data) > maxStage {
		return 0, "", errors.New("single-paper Parquet exceeds staging bound")
	}
	err = r.putImmutable(ctx, key, data)
	if s3Status(err, 412) {
		existing, attrs, err := r.get(ctx, key, maxStage)
		if err != nil {
			return 0, "", err
		}
		return verify(existing, attrs)
	}
	if err != nil {
		return 0, "", err
	}
	log.Printf("staging Parquet uploaded task=%s key=%s rows=%d", t.id, key, len(rows))
	return len(data), sha(data), nil
}

func validVector(v []float32) bool {
	if len(v) != 1536 {
		return false
	}
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
	}
	return true
}
