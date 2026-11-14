package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"local/sci-ingest-pilot/internal/artifact"
)

const repo142 = "/home/dell/projects/TianGong-AI-Unstructure"

type parseTask struct {
	id, journal, doi, mineru string
	attempt                  int
}

func runParser(ctx context.Context, c settings, command string) error {
	token := os.Getenv("SCI_MINERU_TOKEN")
	if token == "" || (c.ParserNode != 140 && c.ParserNode != 141) {
		return errors.New("MinerU token and parser node 140/141 required")
	}
	db, err := pgx.Connect(ctx, c.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parser ledger connection failed: %T", err)
	}
	defer db.Close(context.Background())
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for ctx.Err() == nil {
		var finished bool
		if err := db.QueryRow(ctx, `select sealed and not exists(select 1 from private.sci_ingest_tasks
    where run_id=$1 and parser_node=$2 and local_state not in ('done','failed'))
    from private.sci_backfill_runs where run_id=$1`, c.RunID, c.ParserNode).Scan(&finished); err != nil {
			return err
		}
		if finished {
			return nil
		}
		_, err := db.Exec(ctx, `update private.sci_ingest_tasks set local_state='failed',local_lease=null,local_failed_at=now(),
    last_error='final MinerU submission lease expired'
    where run_id=$1 and parser_node=$2 and local_state='submitting' and local_attempts>=3 and local_lease<=now()`, c.RunID, c.ParserNode)
		if err != nil {
			return err
		}
		if command == "parse-submit" && c.MaxCloudBacklog > 0 {
			var blocked bool
			err := db.QueryRow(ctx, `select exists(select 1 from private.sci_ingest_tasks
			  where run_id=$1 and local_state='done' and cloud_state in ('queued','running')
			  offset $2 limit 1)`, c.RunID, c.MaxCloudBacklog-1).Scan(&blocked)
			if err != nil {
				return err
			}
			if blocked {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(10 * time.Second):
				}
				continue
			}
		}
		did, err := parserStep(ctx, db, client, c, command, token)
		if err != nil {
			return err
		}
		if !did {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
	}
	return ctx.Err()
}

func parserStep(parent context.Context, db *pgx.Conn, client *http.Client, c settings, command, token string) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, 110*time.Second)
	defer cancel()
	var sql string
	if command == "parse-submit" {
		sql = `with candidate as (
    select id from private.sci_ingest_tasks where run_id=$1 and parser_node=$2
    and local_state in ('queued','submitting') and local_attempts<3 and coalesce(local_lease,'-infinity')<=now()
    and (local_state='submitting' or (select count(*) from private.sci_ingest_tasks
      where run_id=$1 and parser_node=$2 and local_state in ('submitting','submitted','finalizing'))<$3)
    order by local_lease nulls first,id limit 1 for update skip locked)
    update private.sci_ingest_tasks t set local_state='submitting',
    local_attempts=t.local_attempts+case when t.local_state='queued' then 1 else 0 end,
    local_lease=clock_timestamp()+interval '2 minutes'
    from candidate where t.id=candidate.id
    returning t.id::text,t.journal_id::text,t.doi,coalesce(t.mineru_task_id::text,''),t.local_attempts,t.local_lease`
	} else {
		sql = `with candidate as (
    select id from private.sci_ingest_tasks where run_id=$1 and parser_node=$2
    and local_state in ('submitted','finalizing') and local_lease<=now()
    order by local_lease,id limit 1 for update skip locked)
    update private.sci_ingest_tasks t set local_state='finalizing',local_lease=clock_timestamp()+interval '2 minutes'
    from candidate where t.id=candidate.id
    returning t.id::text,t.journal_id::text,t.doi,coalesce(t.mineru_task_id::text,''),t.local_attempts,t.local_lease`
	}
	var t parseTask
	var lease time.Time
	args := []any{c.RunID, c.ParserNode}
	if command == "parse-submit" {
		args = append(args, c.ParseCapacity)
	}
	err := db.QueryRow(ctx, sql, args...).Scan(&t.id, &t.journal, &t.doi, &t.mineru, &t.attempt, &lease)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	root := fmt.Sprintf("http://192.168.1.%d:7770/mineru/task", c.ParserNode)
	if command == "parse-submit" {
		taskID, err := mineruSubmit(ctx, client, root, token, t)
		if parent.Err() != nil {
			return true, parent.Err()
		}
		save, stop := context.WithTimeout(parent, 8*time.Second)
		defer stop()
		if err != nil {
			return true, recordParseFailure(save, db, t, lease, err)
		}
		tx, err := db.Begin(save)
		if err != nil {
			return true, err
		}
		defer tx.Rollback(context.Background())
		var msg *int64
		err = tx.QueryRow(save, `update private.sci_ingest_tasks
			set mineru_task_id=$3,local_state='submitted',parse_submitted_at=now(),local_lease=now()+interval '5 seconds'
			where id=$1 and local_lease=$2 and local_state='submitting' returning pgmq_msg_id`, t.id, lease, taskID).Scan(&msg)
		if err != nil {
			return true, err
		}
		if msg != nil {
			if _, err = tx.Exec(save, "select pgmq.archive($1::text,$2::bigint)", parserQueue, *msg); err != nil {
				return true, err
			}
		}
		if err = tx.Commit(save); err != nil {
			return true, err
		}
		log.Printf("MinerU submitted uuid=%s task=%s node=%d", t.journal, taskID, c.ParserNode)
		return true, err
	}
	target := filepath.Join(repo142, "docs", c.rawPrefix(), t.journal+".json.zst")
	stamp, found, err := publishedArtifact(target, t.mineru)
	if err != nil {
		return true, err
	}
	if !found {
		raw, state, err := mineruResult(ctx, client, root, token, t.mineru)
		if err == nil && state != "SUCCESS" {
			var stale bool
			if err := db.QueryRow(ctx, "select coalesce(parse_submitted_at,now()) < now()-interval '30 minutes' from private.sci_ingest_tasks where id=$1", t.id).Scan(&stale); err != nil {
				return true, err
			}
			if stale {
				err = errors.New("MinerU did not finish within 30 minutes")
			}
		}
		if parent.Err() != nil {
			return true, parent.Err()
		}
		save, stop := context.WithTimeout(parent, 8*time.Second)
		defer stop()
		if err != nil {
			return true, recordParseFailure(save, db, t, lease, err)
		}
		if state != "SUCCESS" {
			_, err = db.Exec(save, `update private.sci_ingest_tasks set local_state='submitted',local_lease=now()+interval '5 seconds'
     where id=$1 and local_lease=$2 and local_state='finalizing'`, t.id, lease)
			return true, err
		}
		stamp, err = publishArtifact(target, raw)
		if err != nil {
			return true, err
		}
	}
	save, stop := context.WithTimeout(parent, 8*time.Second)
	defer stop()
	tx, err := db.Begin(save)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(save, `with receipt as (update private.sci_ingest_tasks set local_state='done',local_published_at=$3,
  local_lease=null,scan_at=now()+interval '30 seconds',last_error=null
  where id=$1 and local_lease=$2 and local_state='finalizing' returning journal_id,doi)
  update public.journals j set unstructure_time=$3 from receipt r where j.id=r.journal_id and j.doi=r.doi`, t.id, lease, stamp)
	if err != nil {
		return true, err
	}
	if tag.RowsAffected() != 1 {
		return true, errors.New("local publish receipt lost lease or journal identity")
	}
	if err := tx.Commit(save); err != nil {
		return true, err
	}
	log.Printf("published uuid=%s task=%s", t.journal, t.mineru)
	return true, nil
}

func recordParseFailure(ctx context.Context, db *pgx.Conn, t parseTask, lease time.Time, reason error) error {
	log.Printf("MinerU uuid=%s attempt=%d: %v", t.journal, t.attempt, reason)
	_, err := db.Exec(ctx, `update private.sci_ingest_tasks set local_state=case when local_attempts>=3 then 'failed' else 'queued' end,
  local_lease=case when local_attempts>=3 then null else now()+interval '10 seconds' end,
  mineru_task_id=null,last_error=$3,local_failed_at=case when local_attempts>=3 then now() else null end where id=$1 and local_lease=$2`, t.id, lease, reason.Error())
	return err
}

func mineruSubmit(ctx context.Context, client *http.Client, root, token string, t parseTask) (string, error) {
	if !strings.HasPrefix(t.doi, "10.") || filepath.IsAbs(t.doi) || filepath.Clean(t.doi) != t.doi || !strings.Contains(t.doi, "/") {
		return "", errors.New("unsupported DOI path")
	}
	f, err := os.Open(filepath.Join(repo142, "docs", "journals", t.doi+".pdf"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	var header [5]byte
	if _, err := io.ReadFull(f, header[:]); err != nil || string(header[:]) != "%PDF-" {
		return "", errors.New("source is not a PDF")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err = form.WriteField("tier", "advanced"); err != nil {
		return "", err
	}
	part, err := form.CreateFormFile("file", filepath.Base(f.Name()))
	if err != nil {
		return "", err
	}
	n, err := io.Copy(part, io.LimitReader(f, 128<<20+1))
	if err != nil {
		return "", err
	}
	if n > 128<<20 {
		return "", errors.New("PDF exceeds 128 MiB")
	}
	if err = form.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, root+"?chunk_type=true&return_txt=true", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", fmt.Sprintf("sci-%s-%d", t.id, t.attempt))
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 202 {
		return "", fmt.Errorf("MinerU submit HTTP %d", res.StatusCode)
	}
	var result struct {
		TaskID string `json:"task_id"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result); err != nil || !uuidName.MatchString(result.TaskID) {
		return "", errors.New("MinerU returned no task ID")
	}
	return result.TaskID, nil
}

func mineruResult(ctx context.Context, client *http.Client, root, token, id string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 202 {
		return nil, "", fmt.Errorf("MinerU poll HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > 64<<20 {
		return nil, "", errors.New("MinerU response exceeds 64 MiB")
	}
	var status struct {
		TaskID string `json:"task_id"`
		State  string
	}
	if err = json.Unmarshal(raw, &status); err != nil || status.TaskID != id {
		return nil, "", errors.New("invalid MinerU status or task ID")
	}
	switch status.State {
	case "FAILURE", "REVOKED", "EXPIRED":
		return nil, "", fmt.Errorf("MinerU task %s", status.State)
	case "SUCCESS":
		if _, err := artifact.Parse(raw, id); err != nil {
			return nil, "", err
		}
	}
	return raw, status.State, nil
}

func publishedArtifact(path, id string) (time.Time, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if err != nil {
		return time.Time{}, false, err
	}
	st, err := f.Stat()
	if err != nil {
		return time.Time{}, false, err
	}
	parsed, err := decode(context.Background(), data)
	if err != nil || parsed.TaskID != id {
		return time.Time{}, false, nil
	}
	return st.ModTime(), true, nil
}

func publishArtifact(path string, raw []byte) (time.Time, error) {
	data, err := artifact.Compress(raw)
	if err != nil {
		return time.Time{}, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return time.Time{}, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sci-publish-*")
	if err != nil {
		return time.Time{}, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return time.Time{}, err
	}
	stamp := time.Now()
	if err = os.Chtimes(f.Name(), stamp, stamp); err != nil {
		return time.Time{}, err
	}
	if err = f.Sync(); err != nil {
		return time.Time{}, err
	}
	if err = f.Close(); err != nil {
		return time.Time{}, err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return time.Time{}, err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return time.Time{}, err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return time.Time{}, err
	}
	return stamp, nil
}
