package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"reflect"
)

var mappingProperties = map[string]map[string]string{
	"text": {"type": "text"}, "doi": {"type": "keyword"}, "journal": {"type": "keyword"},
	"date": {"type": "date", "format": "epoch_second"},
}

func (r *runner) prepareIndex(ctx context.Context) error {
	err := r.checkMapping(ctx)
	if err == nil {
		log.Printf("verified existing mapping %s", r.cfg.Index)
		return nil
	}
	var status *httpStatus
	if !errors.As(err, &status) || status.code != 404 {
		return err
	}
	body := map[string]any{"mappings": map[string]any{"properties": mappingProperties}}
	var result struct{ Acknowledged bool }
	if err := r.request(ctx, http.MethodPut, aoss+"/"+r.cfg.Index, body, "", "", &result); err != nil {
		return err
	}
	if !result.Acknowledged {
		return errors.New("OpenSearch did not acknowledge index creation")
	}
	log.Printf("created index %s", r.cfg.Index)
	return r.checkMapping(ctx)
}

func (r *runner) checkMapping(ctx context.Context) error {
	var mapping map[string]struct {
		Mappings struct {
			Properties map[string]map[string]string
		}
	}
	if err := r.request(ctx, http.MethodGet, aoss+"/"+r.cfg.Index+"/_mapping", nil, "", "", &mapping); err != nil {
		return err
	}
	if len(mapping) != 1 || !reflect.DeepEqual(mapping[r.cfg.Index].Mappings.Properties, mappingProperties) {
		return errors.New("mapping differs from the approved four fields")
	}
	return nil
}
