package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/provision"
)

func tableParams(r *http.Request) (string, string) {
	return chi.URLParam(r, config.URLParamSchema), chi.URLParam(r, config.URLParamTable)
}

func (s *server) browse(w http.ResponseWriter, r *http.Request) {
	schema, table := tableParams(r)
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if size == 0 {
		size = config.DataPageSize
	}
	data, err := s.Databases.Browse(r.Context(), urlSlug(r), provision.BrowseRequest{
		Schema: schema, Table: table, Page: page, Size: size, Sort: q.Get("sort"), Desc: q.Get("dir") == "desc",
	})
	s.respond(w, "browse table", data, err)
}

type rowRequest struct {
	Key    model.RowValues `json:"key"`
	Values model.RowValues `json:"values"`
}

func (s *server) insertRow(w http.ResponseWriter, r *http.Request) {
	var req rowRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	schema, table := tableParams(r)
	s.noContent(w, "insert row", s.Databases.InsertRow(r.Context(), urlSlug(r), schema, table, req.Values, actor(r)))
}

func (s *server) updateRow(w http.ResponseWriter, r *http.Request) {
	var req rowRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	schema, table := tableParams(r)
	s.noContent(w, "update row", s.Databases.UpdateRow(r.Context(), urlSlug(r), schema, table, req.Key, req.Values, actor(r)))
}

func (s *server) deleteRow(w http.ResponseWriter, r *http.Request) {
	var req rowRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	schema, table := tableParams(r)
	s.noContent(w, "delete row", s.Databases.DeleteRow(r.Context(), urlSlug(r), schema, table, req.Key, actor(r)))
}
