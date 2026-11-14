package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5"
)

type task struct {
	id, journalID, doi, journal, date, mineru, state string
	attempt                                          int
	fulltext                                         bool
}

const taskFields = "id::text,journal_id::text,doi,journal,published_date,coalesce(mineru_task_id::text,''),cloud_state,cloud_attempts,opensearch_done_at is not null"

func readTask(row pgx.Row) (task, error) {
	var t task
	err := row.Scan(&t.id, &t.journalID, &t.doi, &t.journal, &t.date, &t.mineru, &t.state, &t.attempt, &t.fulltext)
	return t, err
}

func (r *runner) consume(ctx context.Context) error {
	log.Printf("cloud ready run=%s index=%s", r.cfg.RunID, r.cfg.Index)
	nextScan := time.Now()
	// Give S3 notifications a chance to arrive before the first fallback scan.
	if r.cfg.Mode == "smoke" {
		nextScan = nextScan.Add(time.Duration(r.cfg.ScanSeconds) * time.Second)
	}
	for ctx.Err() == nil {
		if !time.Now().Before(nextScan) {
			if err := r.reconcile(ctx); err != nil {
				return err
			}
			nextScan = time.Now().Add(time.Duration(r.cfg.ScanSeconds) * time.Second)
		}
		if err := r.pack(ctx, false); err != nil {
			return err
		}
		var sealed, pending bool
		if err := r.db.QueryRow(ctx, `select sealed, exists(select 1 from private.sci_ingest_tasks
    where run_id=$1 and (local_state not in ('done','failed') or
    (local_state='done' and cloud_state in ('queued','running')))) from private.sci_backfill_runs where run_id=$1`, r.cfg.RunID).Scan(&sealed, &pending); err != nil {
			return err
		}
		if sealed && !pending {
			return r.finishRun(ctx)
		}
		msgs, err := r.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: aws.String(queue), MaxNumberOfMessages: 1, WaitTimeSeconds: 5, VisibilityTimeout: leaseSeconds + 30})
		if err != nil {
			return err
		}
		for _, m := range msgs.Messages {
			owned, done := r.event(ctx, aws.ToString(m.Body))
			if !owned {
				continue
			}
			if done {
				if _, err = r.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(queue), ReceiptHandle: m.ReceiptHandle}); err != nil {
					return err
				}
			} else {
				if _, err = r.sqs.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{QueueUrl: aws.String(queue), ReceiptHandle: m.ReceiptHandle, VisibilityTimeout: 15}); err != nil {
					return err
				}
			}
		}
	}
	return ctx.Err()
}

type objectEvent struct {
	Records []struct {
		EventName string
		S3        struct {
			Bucket struct{ Name string }
			Object struct{ Key string }
		}
	}
}

func (r *runner) event(ctx context.Context, body string) (bool, bool) {
	var e objectEvent
	if json.Unmarshal([]byte(body), &e) != nil || len(e.Records) == 0 {
		return false, false
	}
	ids := make([]string, 0, len(e.Records))
	for _, v := range e.Records {
		key, err := url.QueryUnescape(v.S3.Object.Key)
		if err != nil || v.S3.Bucket.Name != bucket || !strings.HasPrefix(v.EventName, "ObjectCreated:") || !strings.HasPrefix(key, r.cfg.rawPrefix()) || !strings.HasSuffix(key, ".json.zst") {
			return false, false
		}
		id := strings.TrimSuffix(strings.TrimPrefix(key, r.cfg.rawPrefix()), ".json.zst")
		if !uuidName.MatchString(id) {
			return false, false
		}
		ids = append(ids, id)
	}
	allDone := true
	for _, id := range ids {
		t, err := readTask(r.db.QueryRow(ctx, "select "+taskFields+" from private.sci_ingest_tasks where run_id=$1 and journal_id=$2", r.cfg.RunID, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false
		}
		if err != nil {
			log.Printf("event ledger lookup: %v", err)
			return true, false
		}
		log.Printf("sqs notification uuid=%s", id)
		if !r.process(ctx, t, "sqs") {
			allDone = false
		}
	}
	return true, allDone
}

func (r *runner) reconcile(ctx context.Context) error {
	tag, err := r.db.Exec(ctx, `update private.sci_ingest_tasks
  set cloud_state='failed',lease_owner=null,lease_until=null,last_error='worker lease expired on final attempt'
  where run_id=$1 and local_state='done' and cloud_state='running'
  and cloud_attempts>=3 and lease_until<=now()`, r.cfg.RunID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		log.Printf("marked %d final attempts failed", tag.RowsAffected())
	}
	// Move checked rows to the back of the scan so files still in NAS sync do not starve newer work.
	rows, err := r.db.Query(ctx, `with due as (
  select id from private.sci_ingest_tasks where run_id=$1 and local_state='done'
  and cloud_state in ('queued','running') and cloud_attempts<3 and scan_at<=now()
  and coalesce(lease_until,'-infinity')<=now()
  order by scan_at,id limit 4 for update skip locked)
  update private.sci_ingest_tasks t set scan_at=now()+make_interval(secs=>$2)
  from due where t.id=due.id returning t.id::text,t.journal_id::text,t.doi,t.journal,t.published_date,
    coalesce(t.mineru_task_id::text,''),t.cloud_state,t.cloud_attempts,t.opensearch_done_at is not null`, r.cfg.RunID, r.cfg.ScanSeconds)
	if err != nil {
		return err
	}
	var tasks []task
	for rows.Next() {
		t, err := readTask(rows)
		if err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range tasks {
		r.process(ctx, t, "ledger")
	}
	return nil
}

func (r *runner) process(parent context.Context, t task, source string) bool {
	if t.state == "failed" || t.state == "staged" || t.state == "parquet_ready" {
		return true
	}
	if t.mineru == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, 150*time.Second)
	defer cancel()
	key := r.cfg.rawPrefix() + t.journalID + ".json.zst"
	data, _, err := r.get(ctx, key, 64<<20)
	if err != nil {
		if !s3Status(err, 404) {
			log.Printf("raw read %s: %v", t.journalID, err)
		}
		return false
	}
	parsed, err := decode(ctx, data)
	if err != nil {
		log.Printf("raw decode %s: %v", t.journalID, err)
		return false
	}
	if parsed.TaskID != t.mineru {
		return false
	}
	owner := fmt.Sprintf("%s-%d", t.id, time.Now().UnixNano())
	err = r.db.QueryRow(ctx, `update private.sci_ingest_tasks
  set cloud_state='running',cloud_attempts=cloud_attempts+1,lease_owner=$3,
      lease_until=now()+make_interval(secs=>$4)
  where run_id=$1 and id=$2 and local_state='done' and mineru_task_id::text=$5
    and cloud_state in ('queued','running') and cloud_attempts<3 and coalesce(lease_until,'-infinity')<=now()
  returning cloud_attempts`, r.cfg.RunID, t.id, owner, leaseSeconds, t.mineru).Scan(&t.attempt)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("claim %s: %v", t.id, err)
		}
		return false
	}
	log.Printf("claimed source=%s task=%s uuid=%s attempt=%d", source, t.id, t.journalID, t.attempt)
	err = r.processClaim(ctx, t, owner, parsed)
	if err == nil {
		return true
	}
	log.Printf("task %s attempt %d: %v", t.id, t.attempt, err)
	if parent.Err() != nil {
		return false
	}
	save, stop := context.WithTimeout(parent, 8*time.Second)
	defer stop()
	tag, saveErr := r.db.Exec(save, `update private.sci_ingest_tasks set
  cloud_state=case when cloud_attempts>=3 then 'failed' else 'queued' end,
  last_error=$3,lease_owner=null,lease_until=null,scan_at=now()+interval '10 seconds'
  where id=$1 and lease_owner=$2 and lease_until>now() and cloud_state='running'`, t.id, owner, err.Error())
	return saveErr == nil && tag.RowsAffected() == 1 && t.attempt == 3
}

func (r *runner) processClaim(ctx context.Context, t task, owner string, p parserResponse) error {
	chunks, err := r.clean(ctx, p)
	if err != nil {
		return err
	}
	var date time.Time
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		date, err = time.Parse(layout, t.date)
		if err == nil {
			break
		}
	}
	if err != nil {
		return errors.New("unsupported publication date")
	}
	docs := make([]metadata, len(chunks))
	for i, chunk := range chunks {
		docs[i] = metadata{Text: chunk, DOI: t.doi, Journal: t.journal, Date: date.Unix()}
	}
	var fulltextErr error
	if !t.fulltext {
		fulltextErr = r.fulltext(ctx, t, owner, docs)
	}
	size, hash, stageErr := r.ensureStage(ctx, t, docs)
	if fulltextErr != nil || stageErr != nil {
		return errors.Join(fulltextErr, stageErr)
	}
	tag, err := r.db.Exec(ctx, `update private.sci_ingest_tasks set cloud_state='staged',staged_at=now(),stage_bytes=$3,
  stage_sha=$4,indexed_chunks=$5,last_error=null,lease_owner=null,lease_until=null
  where id=$1 and lease_owner=$2 and lease_until>now() and opensearch_done_at is not null`, t.id, owner, size, hash, len(docs))
	if err != nil {
		return fmt.Errorf("save Parquet staging receipt: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("lease lost before saving Parquet staging receipt")
	}
	log.Printf("staging receipt task=%s rows=%d", t.id, len(docs))
	return nil
}

func (r *runner) fulltext(ctx context.Context, t task, owner string, docs []metadata) error {
	for start := 0; start < len(docs); {
		var body bytes.Buffer
		enc := json.NewEncoder(&body)
		end := start
		for ; end < len(docs) && end-start < 500 && body.Len() < 4<<20; end++ {
			if err := enc.Encode(map[string]map[string]string{"index": {"_id": chunkID(t.doi, end), "_index": r.cfg.Index}}); err != nil {
				return err
			}
			if err := enc.Encode(docs[end]); err != nil {
				return err
			}
		}
		var reply struct {
			Errors bool
			Items  []map[string]struct {
				Status int
				Index  string `json:"_index"`
				ID     string `json:"_id"`
			}
		}
		if err := r.request(ctx, http.MethodPost, aoss+"/"+r.cfg.Index+"/_bulk", body.Bytes(), "", "", &reply); err != nil {
			return err
		}
		if reply.Errors || len(reply.Items) != end-start {
			return errors.New("bulk did not confirm every item")
		}
		for i, item := range reply.Items {
			v := item["index"]
			if v.Status < 200 || v.Status >= 300 || v.Index != r.cfg.Index || v.ID != chunkID(t.doi, start+i) {
				return errors.New("bulk receipt mismatch")
			}
		}
		start = end
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(ctx, `with receipt as (
  update private.sci_ingest_tasks set opensearch_done_at=now()
  where id=$1 and lease_owner=$2 and lease_until>now() and cloud_state='running'
  returning journal_id,doi,opensearch_done_at)
  update public.journals j set fulltext_time=r.opensearch_done_at
  from receipt r where j.id=r.journal_id and j.doi=r.doi`, t.id, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("fulltext receipt lost lease or journal identity")
	}
	return tx.Commit(ctx)
}

func (r *runner) get(ctx context.Context, key string, limit int64) ([]byte, map[string]string, error) {
	obj, err := r.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), ExpectedBucketOwner: aws.String(bucketOwner)})
	if err != nil {
		return nil, nil, err
	}
	defer obj.Body.Close()
	b, err := io.ReadAll(io.LimitReader(obj.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("object %s exceeds size limit", key)
	}
	return b, obj.Metadata, err
}
