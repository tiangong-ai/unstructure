package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5"
	"github.com/tiktoken-go/tokenizer"
)

const region = "us-east-1"
const bucket = "tiangong"
const bucketOwner = "339712838008"
const aoss = "https://10p7m4wzg072d8bdrv4.us-east-1.aoss.amazonaws.com"
const queue = "https://sqs.us-east-1.amazonaws.com/339712838008/sci-ingest-pilot-20261111"
const rawRoot = "processed_docs/journal_pickle_v1/"
const importRoot = "processed_docs/pinecone/"
const leaseSeconds = 180

var runName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
var uuidName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type settings struct {
	Mode, RunID, Index, Namespace, DatabaseURL, OpenAIKey string
	ShardBytes                                            int
	ScanSeconds                                           int
	ParserNode, ParseCapacity, MaxCloudBacklog            int
}

func (c settings) validate() error {
	if !runName.MatchString(c.RunID) || c.ShardBytes < 1 || c.ShardBytes > 256<<20 || c.ScanSeconds < 1 || c.ScanSeconds > 300 {
		return errors.New("invalid run, shard size or scan interval")
	}
	switch c.Mode {
	case "smoke":
		if !strings.HasPrefix(c.RunID, "smoke-") || c.Index != "sci_smoke_"+strings.ReplaceAll(c.RunID, "-", "_") || c.Namespace != "sci_test_"+strings.ReplaceAll(c.RunID, "-", "_") {
			return errors.New("smoke destinations must belong to the smoke run")
		}
		db, err := pgx.ParseConfig(c.DatabaseURL)
		if err != nil || db.Host != "127.0.0.1" || !strings.HasPrefix(db.Database, "sci_backfill_smoke_") || db.Port != 15493 {
			return errors.New("smoke requires an isolated loopback database")
		}
	case "backfill":
		if c.Index != "sci_v1" || c.Namespace != "sci_v1" || strings.HasPrefix(c.RunID, "smoke-") {
			return errors.New("expected the approved new targets")
		}
	default:
		return errors.New("explicit mode required")
	}
	return nil
}

func (c settings) rawPrefix() string {
	if c.Mode == "smoke" {
		return rawRoot + c.RunID + "/"
	}
	return rawRoot
}

func (c settings) shardPrefix() string {
	if c.Mode == "smoke" {
		return "processed_docs/pinecone-test/" + c.RunID + "/" + c.Namespace + "/"
	}
	return importRoot + c.Namespace + "/"
}

func (c settings) stagingPrefix() string { return "processed_docs/sci-parquet-stage/" + c.RunID + "/" }

type runner struct {
	db    *pgx.Conn
	s3    *s3.Client
	sqs   *sqs.Client
	creds aws.CredentialsProvider
	http  *http.Client
	cfg   settings
	codec tokenizer.Codec
}

func main() {
	if err := run(); err != nil {
		log.Printf("SCI worker: %v", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 && !(len(os.Args) == 3 && os.Args[1] == "enroll") {
		return errors.New("expected cloud, prepare-index, seal, enroll <csv>, parse-submit, or parse-finalize")
	}
	var c settings
	dec := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return errors.New("invalid configuration")
	}
	if err := c.validate(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if os.Args[1] == "enroll" {
		return enroll(ctx, c, os.Args[2])
	}
	if os.Args[1] == "parse-submit" || os.Args[1] == "parse-finalize" {
		limit := map[int]int{140: 3, 141: 1}[c.ParserNode]
		if c.ParseCapacity < 1 || c.ParseCapacity > limit || (c.Mode == "backfill" && c.MaxCloudBacklog < 1) {
			return errors.New("invalid parser capacity or cloud backlog")
		}
		return runParser(ctx, c, os.Args[1])
	}
	awsc, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return err
	}
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		return err
	}
	r := runner{cfg: c, s3: s3.NewFromConfig(awsc), sqs: sqs.NewFromConfig(awsc), creds: awsc.Credentials, http: &http.Client{Timeout: 50 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, codec: codec}
	if os.Args[1] == "prepare-index" {
		return r.prepareIndex(ctx)
	}
	if os.Args[1] != "cloud" && os.Args[1] != "seal" {
		return errors.New("unknown worker command")
	}
	if os.Args[1] == "cloud" && c.OpenAIKey == "" {
		return errors.New("Embedding key required")
	}
	r.db, err = pgx.Connect(ctx, c.DatabaseURL)
	if err != nil {
		return fmt.Errorf("ledger connection failed: %T", err)
	}
	defer r.db.Close(context.Background())
	if os.Args[1] == "seal" {
		return r.seal(ctx)
	}
	lock, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	lockKey := "sci-backfill:" + c.RunID
	if c.Mode == "backfill" {
		lockKey = "sci-backfill:" + c.Index
	}
	if _, err = r.db.Exec(lock, `select pg_advisory_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return fmt.Errorf("acquire worker lock: %w", err)
	}

	if err = r.checkMapping(ctx); err != nil {
		return err
	}
	return r.consume(ctx)
}

type httpStatus struct {
	method, path string
	code         int
}

func (e *httpStatus) Error() string { return fmt.Sprintf("%s %s HTTP %d", e.method, e.path, e.code) }

func (r *runner) request(ctx context.Context, method, endpoint string, body any, header, token string, output any) error {
	var raw []byte
	var err error
	switch v := body.(type) {
	case nil:
	case []byte:
		raw = v
	default:
		raw, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.HasSuffix(req.URL.Path, "/_bulk") {
		req.Header.Set("Content-Type", "application/x-ndjson")
	}
	if header != "" {
		req.Header.Set(header, token)
	}
	if strings.HasPrefix(endpoint, aoss+"/") {
		path := "/" + r.cfg.Index
		if !(method == http.MethodPut && req.URL.Path == path) && !strings.HasPrefix(req.URL.Path, path+"/") {
			return errors.New("OpenSearch request outside selected new index")
		}
		credentials, err := r.creds.Retrieve(ctx)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		hash := hex.EncodeToString(digest[:])
		req.Header.Set("x-amz-content-sha256", hash)
		if err = v4.NewSigner().SignHTTP(ctx, credentials, req, hash, "aoss", region, time.Now()); err != nil {
			return err
		}
	}
	res, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if req.URL.Host == "api.openai.com" {
		log.Printf("embedding HTTP=%d request_id=%s rpm=%s tpm=%s project_tpm=%s", res.StatusCode, res.Header.Get("x-request-id"), res.Header.Get("x-ratelimit-limit-requests"), res.Header.Get("x-ratelimit-limit-tokens"), res.Header.Get("x-ratelimit-limit-project-tokens"))
		if res.StatusCode == 429 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Second):
			}
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &httpStatus{method, req.URL.Path, res.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(res.Body, 48<<20)).Decode(output)
}
