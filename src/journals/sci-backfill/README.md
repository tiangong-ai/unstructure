# SCI historical journal backfill

This Go command prepares historical SCI journals for the new OpenSearch index and writes Pinecone-compatible Parquet to S3. Production search continues to use its existing index and namespace until a separate cutover.

| Command | Host | Result |
|---|---|---|
| `prepare-index` | aws-soda | Creates or validates the configured new index and the four-field mapping. |
| `enroll <manifest.csv>` | 142 | Resolves relative PDF paths to journal UUIDs, assigns node 140 or 141, and writes PGMQ messages in the same transaction. |
| `parse-submit`, `parse-finalize` | 142 | Submit advanced typed MinerU tasks, publish the original compressed JSON, and save the publication receipt. Run one process per command per node. |
| `cloud` | aws-soda | Consumes SCI SQS, scans the task ledger for missed notifications, and writes OpenSearch and Parquet. |
| `seal` | 142 or aws-soda | Closes a run to further enrollment. Once all tasks are terminal, the cloud worker flushes the last shard and verifies the S3 file list. |

Build with `go build -trimpath -o sci-backfill .`. All commands take one JSON configuration on stdin. Use an operator-provided secret source and keep credentials out of files, shell arguments, and logs. The cloud commands use the AWS SDK credential chain. The parser reads `SCI_MINERU_TOKEN` from its environment. For the production ledger use an authorized direct PostgreSQL endpoint with TLS, or a session pooler that supports advisory locks.

```json
{
  "Mode": "backfill",
  "RunID": "sci-historical-v1",
  "Index": "sci_v1",
  "Namespace": "sci_v1",
  "DatabaseURL": "<read from runtime secret>",
  "OpenAIKey": "<read from runtime secret>",
  "ShardBytes": 134217728,
  "ScanSeconds": 25,
  "ParserNode": 140,
  "ParseCapacity": 3,
  "MaxCloudBacklog": 100
}
```

`OpenAIKey` is required by `cloud`. Set `ParserNode` to 141 and `ParseCapacity` to 1 for the second MinerU node. `MaxCloudBacklog` bounds the parsed tasks waiting for cloud work, with room for in-flight MinerU tasks. `ShardBytes` is the target based on the sum of source Parquet bytes. The final shard is flushed when the run finishes.

Use CSV columns `pdf_path,node`. Supply the original path relative to `docs/journals/`, for example `10.1016/0006-3207%252869%252990181-5.pdf`. Enrollment strips `.pdf`, URL-decodes twice, then resolves the exact DOI to `public.journals.id`. The submit worker restores the same encoded filename using the historical encoding rule. The existing `uuid,node` header also works. Enrollment is transactional per 100 rows and can resume with the same manifest before sealing. Verify each source file and its size before enrollment and use disjoint journal lists for successive batches that share an import directory. Resolve duplicate DOIs and missing journal metadata before enrollment.

Before activation, inspect the production ledger and apply the reviewed `schema.sql`. Verify the SCI PGMQ queue exists, prior SCI messages are drained, and the legacy SCI consumer is stopped before enrolling new jobs. The old worker reads the same PGMQ queue and would archive unfamiliar IDs. The new parser uses the ledger for bounded claims and archives its PGMQ entry when the MinerU task ID is durable. This release candidate does not migrate the production ledger or start either parser process. Use a service supervisor to restart a stopped cloud worker. A cloud session lock prevents two workers in the same ledger from writing the new index simultaneously.

The parser publishes to the existing `journal_pickle_v1/<UUID>.json.zst` path. The cloud worker compares the MinerU task ID before claiming, and checks for eligible S3 objects even if the event is missing. A missing object, stale version, or unsynced artifact does not consume an attempt. Monitor those rows and the SQS oldest-message age. Expired final attempts are marked failed. An OpenSearch receipt and `fulltext_time` commit together. Successful fulltext writes are reused if the vector branch fails.

The per-paper vector result is saved under `processed_docs/sci-parquet-stage/<run>/`, outside the import root. On retry the worker validates the existing file against the task and reuses it. The ledger reserves a shard ID and member set before uploading to `processed_docs/pinecone/sci_v1/`. Conditional uploads preserve existing objects. Shard and task receipts commit together. At completion, the worker checks the current run's vector count against its task receipts, then checks the S3 file names and sizes against uploaded shard receipts from all backfill runs. Keep earlier shards in this directory so the manual import includes the papers from every batch. Keep stage objects through final verification and manual import. Set `embedding_time` after successful manual import and acceptance.

For the smoke test, the same binary selects `Mode=smoke` with a dedicated test prefix, index, and loopback ledger. The cloud check reused existing parser results. It did not activate the production parser, test the production database migration, or import into Pinecone. See [the operational runbook](../../../_docs/runbooks/development.md).
