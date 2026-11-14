package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
)

const parserQueue = "kb_sci_parse_queue"

// Enrollment and its PGMQ wake-up commit together.
func enroll(ctx context.Context, c settings, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = 2
	header, err := reader.Read()
	if err != nil || (header[0] != "uuid" && header[0] != "pdf_path") || header[1] != "node" {
		return errors.New("expected CSV columns pdf_path,node or uuid,node")
	}
	byPath := header[0] == "pdf_path"
	db, err := pgx.Connect(ctx, c.DatabaseURL)
	if err != nil {
		return fmt.Errorf("enrollment connection failed: %T", err)
	}
	defer db.Close(context.Background())
	total := 0
	for {
		keys, nodes := make([]string, 0, 100), make([]int32, 0, 100)
		for len(keys) < 100 {
			row, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			key := row[0]
			if byPath {
				key, err = doiFromPDFPath(key)
			}
			if err != nil || (!byPath && !uuidName.MatchString(key)) || (row[1] != "140" && row[1] != "141") {
				return fmt.Errorf("invalid enrollment row %d", total+len(keys)+2)
			}
			node := int32(140)
			if row[1] == "141" {
				node = 141
			}
			keys, nodes = append(keys, key), append(nodes, node)
		}
		if len(keys) == 0 {
			break
		}
		if err = enrollBatch(ctx, db, c.RunID, keys, nodes, byPath); err != nil {
			return err
		}
		total += len(keys)
	}
	if total == 0 {
		return errors.New("empty enrollment manifest")
	}
	fmt.Printf("validated enrollment run=%s rows=%d\n", c.RunID, total)
	return nil
}

func doiFromPDFPath(pdfPath string) (string, error) {
	doi, err := url.PathUnescape(strings.TrimSuffix(pdfPath, ".pdf"))
	if err != nil {
		return "", err
	}
	doi, err = url.PathUnescape(doi)
	if err != nil || !validDOIPath(doi) || encodedPDFPath(doi) != pdfPath {
		return "", errors.New("invalid encoded PDF path")
	}
	return doi, nil
}

func validDOIPath(doi string) bool {
	return strings.HasPrefix(doi, "10.") && strings.Contains(doi, "/") &&
		!strings.ContainsAny(doi, "\x00\\") && path.Clean(doi) == doi
}

// The historical writer used Python quote(quote(doi)), preserving "/" both times.
func encodedPDFPath(doi string) string {
	const hex = "0123456789ABCDEF"
	var encoded strings.Builder
	for i := 0; i < len(doi); i++ {
		b := doi[i]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
			b >= '0' && b <= '9' || strings.ContainsRune("-._~/", rune(b)) {
			encoded.WriteByte(b)
		} else {
			encoded.WriteString("%25")
			encoded.WriteByte(hex[b>>4])
			encoded.WriteByte(hex[b&15])
		}
	}
	return encoded.String() + ".pdf"
}

func enrollBatch(ctx context.Context, db *pgx.Conn, run string, ids []string, nodes []int32, byPath bool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `insert into private.sci_backfill_runs(run_id) values($1) on conflict do nothing`, run); err != nil {
		return err
	}
	var sealed bool
	if err = tx.QueryRow(ctx, `select sealed from private.sci_backfill_runs where run_id=$1 for update`, run).Scan(&sealed); err != nil {
		return err
	}
	if sealed {
		return errors.New("run sealed")
	}
	if byPath {
		var resolved []string
		err = tx.QueryRow(ctx, `select array_agg(coalesce(j.id::text,'') order by x.ord)
			from unnest($1::text[]) with ordinality as x(doi,ord)
			left join public.journals j on j.doi=x.doi`, ids).Scan(&resolved)
		if err != nil {
			return err
		}
		if len(resolved) != len(ids) {
			return errors.New("decoded DOI matched more than one journal UUID")
		}
		for i, id := range resolved {
			if !uuidName.MatchString(id) {
				return fmt.Errorf("decoded DOI %q does not match a journal UUID", ids[i])
			}
		}
		ids = resolved
	}
	var valid int
	if err = tx.QueryRow(ctx, `select count(*) from unnest($1::text[]::uuid[],$2::int[]) as x(id,node)
	   join public.journals j on j.id=x.id
	   where j.doi<>'' and j.journal is not null and j.date is not null
	    and not exists(select 1 from private.sci_ingest_tasks t where t.run_id=$3 and t.journal_id=x.id and t.parser_node<>x.node)`, ids, nodes, run).Scan(&valid); err != nil {
		return err
	}
	if valid != len(ids) {
		return errors.New("unknown journal, missing metadata, or parser assignment changed")
	}
	_, err = tx.Exec(ctx, `insert into private.sci_ingest_tasks(run_id,journal_id,doi,journal,published_date,parser_node)
  select $3,j.id,j.doi,j.journal,j.date,x.node
  from unnest($1::text[]::uuid[],$2::int[]) as x(id,node) join public.journals j on j.id=x.id
  on conflict(run_id,journal_id) do nothing`, ids, nodes, run)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `with messages as materialized (
	  select t.id,m.msg_id from private.sci_ingest_tasks t
	  cross join lateral pgmq.send($3::text,jsonb_build_object('job_id',t.id,'pipeline','sci-journal-backfill-v1','run_id',t.run_id,'node',t.parser_node)) as m(msg_id)
	  where t.run_id=$2 and t.journal_id=any($1::text[]::uuid[]) and t.pgmq_msg_id is null and t.local_state='queued'
	)
	update private.sci_ingest_tasks t set pgmq_msg_id=m.msg_id from messages m where t.id=m.id`, ids, run, parserQueue)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
