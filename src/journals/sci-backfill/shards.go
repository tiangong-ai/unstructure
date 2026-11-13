package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"time"

	"github.com/jackc/pgx/v5"
	parquet "github.com/parquet-go/parquet-go"
)

type shardMember struct {
	id, doi, sha string
	bytes        int64
	rows         int
}

func (r *runner) seal(ctx context.Context) error {
	tag, err := r.db.Exec(ctx, "update private.sci_backfill_runs set sealed=true where run_id=$1", r.cfg.RunID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("unknown run")
	}
	return nil
}

func (r *runner) finishRun(ctx context.Context) error {
	for {
		var remaining bool
		if err := r.db.QueryRow(ctx, `select exists(select 1 from private.sci_ingest_tasks where run_id=$1 and cloud_state='staged')`, r.cfg.RunID).Scan(&remaining); err != nil {
			return err
		}
		if !remaining {
			break
		}
		if err := r.pack(ctx, true); err != nil {
			return err
		}
	}
	var papers, failed, shards int
	var rows, bytes int64
	if err := r.db.QueryRow(ctx, `select count(*) filter (where cloud_state='parquet_ready'), count(*) filter (where local_state='failed' or cloud_state='failed') from private.sci_ingest_tasks where run_id=$1`, r.cfg.RunID).Scan(&papers, &failed); err != nil {
		return err
	}
	if err := r.db.QueryRow(ctx, `select count(*),coalesce(sum(vector_count),0),coalesce(sum(bytes),0) from private.sci_backfill_shards where run_id=$1 and uploaded_at is not null`, r.cfg.RunID).Scan(&shards, &rows, &bytes); err != nil {
		return err
	}
	log.Printf("run=%s finished papers=%d failed=%d shards=%d rows=%d bytes=%d", r.cfg.RunID, papers, failed, shards, rows, bytes)
	if failed != 0 {
		return errors.New("run includes failed papers; resolve coverage before manual import")
	}
	if papers == 0 {
		return errors.New("run contains no successful papers")
	}
	return r.verifyImportObjects(ctx, shards, rows, bytes)
}

func (r *runner) pack(parent context.Context, flush bool) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	var id int64
	err := r.db.QueryRow(ctx, `select id from private.sci_backfill_shards where run_id=$1 and uploaded_at is null order by id limit 1`, r.cfg.RunID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = r.reserveShard(ctx, flush)
	}
	if err != nil || id == 0 {
		return err
	}
	members, err := r.shardMembers(ctx, id)
	if err != nil {
		return err
	}
	key := r.shardKey(id)
	var out bytes.Buffer
	writer := parquet.NewGenericWriter[parquetRow](&out, parquet.MaxRowsPerRowGroup(4096))
	var rowCount int64
	for _, m := range members {
		data, _, err := r.get(ctx, r.cfg.stagingPrefix()+m.id+".parquet", maxStage)
		if err != nil {
			return err
		}
		if int64(len(data)) != m.bytes || sha(data) != m.sha {
			return fmt.Errorf("staging receipt mismatch for %s", m.id)
		}
		rows, err := readParquet(data, m.rows)
		if err != nil {
			return err
		}
		for i, row := range rows {
			if row.ID != chunkID(m.doi, i) {
				return errors.New("shard chunk IDs differ from ledger")
			}
		}
		if _, err = writer.Write(rows); err != nil {
			return err
		}
		rowCount += int64(len(rows))
	}
	if err = writer.Close(); err != nil {
		return err
	}
	data := out.Bytes()
	digest := sha(data)
	err = r.putImmutable(ctx, key, data)
	if s3Status(err, 412) {
		existing, attrs, getErr := r.get(ctx, key, int64(len(data)))
		if getErr != nil {
			return getErr
		}
		if attrs["sha256"] != digest || !bytes.Equal(data, existing) {
			return errors.New("existing shard differs from recorded members")
		}
		log.Printf("S3 conditional PUT returned 412; reused uploaded shard key=%s", key)
	} else if err != nil {
		return err
	} else {
		log.Printf("shard uploaded key=%s papers=%d rows=%d bytes=%d", key, len(members), rowCount, len(data))
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(ctx, `update private.sci_backfill_shards set uploaded_at=now(),bytes=$3,sha=$4,vector_count=$5
  where run_id=$1 and id=$2 and uploaded_at is null`, r.cfg.RunID, id, len(data), digest, rowCount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("shard receipt lost")
	}
	tag, err = tx.Exec(ctx, `update private.sci_ingest_tasks set cloud_state='parquet_ready',parquet_at=now()
  where run_id=$1 and shard_id=$2 and cloud_state='staged'`, r.cfg.RunID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != int64(len(members)) {
		return errors.New("shard membership changed")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	log.Printf("shard receipt id=%d papers=%d rows=%d", id, len(members), rowCount)
	return nil
}

func (r *runner) reserveShard(ctx context.Context, flush bool) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `select id::text,stage_bytes from private.sci_ingest_tasks
  where run_id=$1 and cloud_state='staged' and shard_id is null order by staged_at,id limit 8192`, r.cfg.RunID)
	if err != nil {
		return 0, err
	}
	var ids []string
	var size int64
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		size += n
		if size >= int64(r.cfg.ShardBytes) {
			break
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 || !flush && size < int64(r.cfg.ShardBytes) && len(ids) < 8192 {
		return 0, nil
	}
	var id int64
	if err = tx.QueryRow(ctx, `insert into private.sci_backfill_shards(run_id) values($1) returning id`, r.cfg.RunID).Scan(&id); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `update private.sci_ingest_tasks set shard_id=$2 where run_id=$1 and id=any($3::text[]::uuid[]) and shard_id is null and cloud_state='staged'`, r.cfg.RunID, id, ids)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return 0, errors.New("failed to reserve every shard member")
	}
	return id, tx.Commit(ctx)
}

func (r *runner) shardMembers(ctx context.Context, id int64) ([]shardMember, error) {
	rows, err := r.db.Query(ctx, `select id::text,doi,stage_sha,stage_bytes,indexed_chunks
  from private.sci_ingest_tasks where run_id=$1 and shard_id=$2 and cloud_state='staged' order by doi`, r.cfg.RunID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []shardMember
	for rows.Next() {
		var m shardMember
		if err = rows.Scan(&m.id, &m.doi, &m.sha, &m.bytes, &m.rows); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, errors.New("pending shard has no members")
	}
	return members, nil
}

func (r *runner) shardKey(id int64) string {
	return fmt.Sprintf("%spart-%s-%012d.parquet", r.cfg.shardPrefix(), r.cfg.RunID, id)
}

func (r *runner) verifyImportObjects(ctx context.Context, shards int, vectors, totalBytes int64) error {
	if shards < 1 || shards > 100_000 || totalBytes > 1_000_000_000_000 {
		return errors.New("Parquet export outside Pinecone import limits")
	}
	var taskRows int64
	if err := r.db.QueryRow(ctx, `select coalesce(sum(indexed_chunks),0) from private.sci_ingest_tasks where run_id=$1 and cloud_state='parquet_ready'`, r.cfg.RunID).Scan(&taskRows); err != nil {
		return err
	}
	if taskRows != vectors {
		return errors.New("shard row total differs from task receipts")
	}
	objects, err := r.db.Query(ctx, `select id,bytes from private.sci_backfill_shards where run_id=$1 and uploaded_at is not null`, r.cfg.RunID)
	if err != nil {
		return err
	}
	expected := make(map[string]int64, shards)
	for objects.Next() {
		var id, n int64
		if err = objects.Scan(&id, &n); err != nil {
			objects.Close()
			return err
		}
		expected[r.shardKey(id)] = n
	}
	objects.Close()
	if err = objects.Err(); err != nil {
		return err
	}
	p := s3.NewListObjectsV2Paginator(r.s3, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), ExpectedBucketOwner: aws.String(bucketOwner), Prefix: aws.String(r.cfg.shardPrefix())})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, file := range page.Contents {
			key := aws.ToString(file.Key)
			if expected[key] <= 0 || expected[key] != aws.ToInt64(file.Size) {
				return fmt.Errorf("unexpected import object %s", key)
			}
			delete(expected, key)
		}
	}
	if len(expected) != 0 {
		return errors.New("missing import objects")
	}
	log.Printf("verified import objects shards=%d vectors=%d bytes=%d", shards, vectors, totalBytes)
	return nil
}
