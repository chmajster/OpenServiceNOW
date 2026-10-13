package cmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type PermissionProvider interface {
	Permissions(context.Context, identity.SessionIdentity) ([]string, error)
}
type Handler struct {
	service     *Service
	permissions PermissionProvider
}

func NewHandler(s *Service, p PermissionProvider) *Handler { return &Handler{s, p} }

type actorKey struct{}

func (h *Handler) context(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		session, ok := identity.FromContext(r.Context())
		if !ok {
			reply(w, r, nil, workrecords.ErrForbidden)
			return
		}
		keys, err := h.permissions.Permissions(r.Context(), session)
		if err != nil {
			reply(w, r, nil, err)
			return
		}
		a := Actor{Actor: workrecords.Actor{TenantID: session.TenantID, UserID: session.UserID, RequestID: middleware.GetReqID(r.Context())}, IPAddress: requestinfo.ClientIP(r), Permissions: map[string]bool{}}
		for _, k := range keys {
			a.Permissions[k] = true
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}
func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/cmdb", func(r chi.Router) {
		r.Use(m.Authenticate, h.context)
		r.Get("/classes", h.handle("classes"))
		r.Get("/lookups/users", h.handle("users"))
		r.Get("/lookups/groups", h.handle("groups"))
		r.Get("/cis", h.handle("ci.list"))
		r.Get("/cis/{id}", h.handle("ci.get"))
		r.With(m.RequireCSRF).Post("/cis", h.handle("ci.save"))
		r.With(m.RequireCSRF).Put("/cis/{id}", h.handle("ci.save"))
		r.With(m.RequireCSRF).Post("/cis/{id}/archive", h.handle("ci.archive"))
		r.Get("/cis/{id}/history", h.handle("ci.history"))
		r.Get("/cis/{id}/relations", h.handle("relations"))
		r.With(m.RequireCSRF).Post("/cis/{id}/relations", h.handle("relation.add"))
		r.With(m.RequireCSRF).Delete("/cis/{id}/relations/{relationID}", h.handle("relation.remove"))
		r.Get("/cis/{id}/graph", h.handle("graph"))
		r.Get("/cis/{id}/asset", h.handle("asset.link"))
		r.With(m.RequireCSRF).Put("/cis/{id}/asset", h.handle("asset.set"))
		r.Get("/cis/{id}/records/{kind:incidents|requests}", h.handle("records"))
		r.Get("/assets", h.handle("asset.list"))
		r.Get("/assets/{id}", h.handle("asset.get"))
		r.With(m.RequireCSRF).Post("/assets", h.handle("asset.save"))
		r.With(m.RequireCSRF).Put("/assets/{id}", h.handle("asset.save"))
		r.With(m.RequireCSRF).Post("/assets/{id}/archive", h.handle("asset.archive"))
		r.Get("/assets/{id}/history", h.handle("asset.history"))
		r.Get("/assets/{id}/ci", h.handle("asset.ci"))
		r.Get("/records/{kind:incidents|requests}/{id}/cis", h.handle("record.cis"))
		r.With(m.RequireCSRF).Put("/records/{kind:incidents|requests}/{id}/cis/{ciID}", h.handle("record.link"))
		r.With(m.RequireCSRF).Delete("/records/{kind:incidents|requests}/{id}/cis/{ciID}", h.handle("record.unlink"))
		r.Get("/imports", h.handle("imports"))
		r.With(m.RequireCSRF).Post("/imports", h.handle("import.create"))
		r.Get("/imports/{id}", h.handle("import.get"))
		r.Get("/imports/{id}/rows", h.handle("import.rows"))
		r.With(m.RequireCSRF).Post("/imports/{id}/{action:apply|review|cancel}", h.handle("import.action"))
	})
}
func number(v string, optional bool) (uint64, error) {
	if optional && v == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != v {
		return 0, ErrInvalid
	}
	return n, nil
}
func pagination(r *http.Request) (uint64, int, error) {
	n, err := number(r.URL.Query().Get("before_id"), true)
	limit := 25
	if v := r.URL.Query().Get("limit"); v != "" {
		var e error
		limit, e = strconv.Atoi(v)
		if e != nil {
			return 0, 0, ErrInvalid
		}
	}
	if err != nil || limit < 1 || limit > 100 {
		return 0, 0, ErrInvalid
	}
	return n, limit, nil
}
func reply(w http.ResponseWriter, r *http.Request, value any, err error) {
	if err == nil {
		httpapi.WriteJSON(w, 200, value)
		return
	}
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	status, code, message := 500, "INVENTORY_FAILED", "Nie udało się wykonać operacji ewidencji"
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, workrecords.ErrInvalid), errors.Is(err, attachments.ErrInvalid):
		status, code, message = 400, "INVALID_REQUEST", err.Error()
	case errors.Is(err, ErrNotFound), errors.Is(err, workrecords.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "Nie znaleziono dostępnego rekordu"
	case errors.Is(err, workrecords.ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "Brak uprawnień do tej operacji"
	case errors.Is(err, ErrConflict), errors.Is(err, ErrDuplicate), errors.Is(err, workrecords.ErrOperationConflict):
		status, code, message = 409, "VERSION_CONFLICT", err.Error()
	case errors.Is(err, attachments.ErrTooLarge):
		status, code, message = 413, "REQUEST_TOO_LARGE", "Plik przekracza limit"
	case errors.Is(err, database.ErrCommitUncertain):
		status, code, message = 503, "COMMIT_UNCERTAIN", "Nie potwierdzono wyniku zapisu. Odczytaj stan albo ponów utworzenie z tym samym kluczem operacji."
	}
	httpapi.WriteError(w, r, status, code, message)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := httpapi.DecodeJSON(r, v); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return false
	}
	return true
}
func (h *Handler) handle(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, _ := r.Context().Value(actorKey{}).(Actor)
		ctx := r.Context()
		id, err := number(chi.URLParam(r, "id"), true)
		if err != nil {
			reply(w, r, nil, err)
			return
		}
		before, limit, err := pagination(r)
		if err != nil {
			reply(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		var v any
		switch action {
		case "classes":
			if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") && !a.Has("assets.read") && !a.Has("assets.read_own") {
				err = workrecords.ErrForbidden
				break
			}
			v = map[string]any{"classes": Classes, "ci_import_fields": ImportFields("CI"), "asset_import_fields": ImportFields("ASSET"), "max_csv_bytes": MaxCSVBytes, "max_csv_rows": MaxCSVRows, "max_csv_columns": MaxCSVColumns}
		case "users":
			v, err = h.users(ctx, a, q.Get("q"), before, limit)
		case "groups":
			v, err = h.groups(ctx, a, q.Get("q"), before, limit)
		case "ci.list":
			var owner uint64
			owner, err = number(q.Get("owner_id"), true)
			if err == nil {
				var node *recordlists.Node
				node, err = recordlists.Decode(q.Get("filter"))
				if err != nil {
					err = ErrInvalid
					break
				}
				f := Filter{Query: q.Get("q"), Class: q.Get("class"), Status: q.Get("status"), Environment: q.Get("environment"), Location: q.Get("location"), Archived: q.Get("archived"), OwnerID: owner, Before: before, Limit: limit, Expression: node, Sort: q.Get("sort"), GroupBy: q.Get("group_by"), BeforeValue: q.Get("before_value")}
				if q.Get("format") == "csv" {
					h.export(w, r, a, f)
					return
				}
				if q.Get("format") != "" {
					err = ErrInvalid
					break
				}
				v, err = h.service.List(ctx, a, f)
			}
		case "asset.list":
			var owner uint64
			owner, err = number(q.Get("owner_id"), true)
			if err == nil {
				v, err = h.service.Assets.List(ctx, a, assets.Filter{Query: q.Get("q"), State: q.Get("state"), Location: q.Get("location"), Archived: q.Get("archived"), OwnerID: owner, Before: before, Limit: limit})
			}
		case "ci.get":
			v, err = h.service.Get(ctx, a, id)
		case "asset.get":
			v, err = h.service.Assets.Get(ctx, a, id)
		case "ci.save":
			var body struct {
				Fields
				Version uint64 `json:"version,string"`
			}
			if !decode(w, r, &body) {
				return
			}
			v, err = h.service.Save(ctx, a, id, Record{Fields: body.Fields, Version: body.Version}, r.Header.Get("Idempotency-Key"))
		case "asset.save":
			var body struct {
				assets.Fields
				Version uint64 `json:"version,string"`
			}
			if !decode(w, r, &body) {
				return
			}
			v, err = h.service.Assets.Save(ctx, a, id, assets.Record{Fields: body.Fields, Version: body.Version}, r.Header.Get("Idempotency-Key"))
		case "ci.archive", "asset.archive":
			var body struct {
				Version  uint64 `json:"version,string"`
				Archived bool   `json:"archived"`
				Reason   string `json:"reason"`
			}
			if !decode(w, r, &body) {
				return
			}
			if action == "ci.archive" {
				v, err = h.service.Archive(ctx, a, id, body.Version, body.Archived, body.Reason)
			} else {
				v, err = h.service.Assets.Archive(ctx, a, id, body.Version, body.Archived, body.Reason)
			}
		case "ci.history", "asset.history":
			kind := "ci"
			if action == "asset.history" {
				kind = "asset"
			}
			v, err = h.service.History(ctx, a, kind, id, before, limit)
		case "relations":
			v, err = h.service.Relations(ctx, a, id, before, limit)
		case "relation.add":
			var body RelationInput
			if !decode(w, r, &body) {
				return
			}
			v, err = h.service.AddRelation(ctx, a, id, body, r.Header.Get("Idempotency-Key"))
		case "relation.remove":
			var body struct {
				Version       uint64 `json:"version,string"`
				TargetVersion uint64 `json:"target_version,string"`
			}
			if !decode(w, r, &body) {
				return
			}
			var relation uint64
			relation, err = number(chi.URLParam(r, "relationID"), false)
			if err == nil {
				v, err = h.service.RemoveRelation(ctx, a, id, relation, body.Version, body.TargetVersion)
			}
		case "graph":
			depth, nodes := 2, 50
			for key, dst := range map[string]*int{"depth": &depth, "max_nodes": &nodes} {
				if q.Get(key) != "" {
					*dst, err = strconv.Atoi(q.Get(key))
					if err != nil {
						err = ErrInvalid
						break
					}
				}
			}
			if err == nil {
				v, err = h.service.Graph(ctx, a, id, GraphFilter{Class: q.Get("class"), Environment: q.Get("environment"), Type: q.Get("type"), MaxNodes: nodes, Depth: depth})
			}
		case "asset.link":
			v, err = h.service.AssetLink(ctx, a, id)
		case "asset.ci":
			v, err = h.service.AssetCI(ctx, a, id)
		case "asset.set":
			var body AssetLinkInput
			if !decode(w, r, &body) {
				return
			}
			v, err = h.service.SetAsset(ctx, a, id, body)
		case "records":
			v, err = h.service.Records(ctx, a, chi.URLParam(r, "kind"), q.Get("q"), id, before, limit)
		case "record.cis":
			v, err = h.service.RecordCIs(ctx, a, chi.URLParam(r, "kind"), id, before, limit)
		case "record.link", "record.unlink":
			var body RecordLinkInput
			if !decode(w, r, &body) {
				return
			}
			var ci uint64
			ci, err = number(chi.URLParam(r, "ciID"), false)
			if err == nil {
				v, err = h.service.ChangeRecordLink(ctx, a, chi.URLParam(r, "kind"), id, ci, body, action == "record.unlink", r.Header.Get("Idempotency-Key"))
			}
		case "imports":
			v, err = h.service.ListImports(ctx, a, before, limit)
		case "import.get":
			v, err = h.service.GetImport(ctx, a, id)
		case "import.rows":
			after := 0
			if q.Get("after_row") != "" {
				after, err = strconv.Atoi(q.Get("after_row"))
			}
			if err == nil {
				v, err = h.service.ImportRows(ctx, a, id, after, limit)
			} else {
				err = ErrInvalid
			}
		case "import.action":
			var body struct {
				Version uint64 `json:"version,string"`
			}
			if !decode(w, r, &body) {
				return
			}
			v, err = h.service.ImportAction(ctx, a, id, body.Version, chi.URLParam(r, "action"))
		case "import.create":
			v, err = h.upload(w, r, a)
		default:
			err = ErrInvalid
		}
		reply(w, r, v, err)
	}
}
func (h *Handler) upload(w http.ResponseWriter, r *http.Request, a Actor) (Import, error) {
	if !importAllowed(a, "CI") && !importAllowed(a, "ASSET") {
		return Import{}, workrecords.ErrForbidden
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxCSVBytes+65536)
	if err := r.ParseMultipartForm(MaxCSVBytes + 65536); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return Import{}, attachments.ErrTooLarge
		}
		return Import{}, ErrInvalid
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 || len(r.MultipartForm.Value) != 1 || len(r.MultipartForm.Value["config"]) != 1 {
		return Import{}, ErrInvalid
	}
	var config ImportConfig
	decoder := json.NewDecoder(strings.NewReader(r.FormValue("config")))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		return Import{}, ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Import{}, ErrInvalid
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return Import{}, ErrInvalid
	}
	defer file.Close()
	if header.Size > MaxCSVBytes {
		return Import{}, attachments.ErrTooLarge
	}
	return h.service.CreateImport(r.Context(), a, config, header.Filename, r.Header.Get("Idempotency-Key"), file)
}
func (h *Handler) users(ctx context.Context, a Actor, q string, before uint64, limit int) (any, error) {
	if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") && !assets.Writable(a) {
		return nil, workrecords.ErrForbidden
	}
	if !assets.Text(q, 0, 160) {
		return nil, ErrInvalid
	}
	query := `SELECT u.id,u.display_name FROM users u JOIN tenant_memberships m ON m.user_id=u.id WHERE m.tenant_id=? AND m.active=TRUE AND u.active=TRUE`
	args := []any{a.TenantID}
	if !a.Has("cmdb.read") && !assets.Writable(a) {
		query += " AND u.id=?"
		args = append(args, a.UserID)
	}
	if q != "" {
		query += " AND LOCATE(?,u.display_name)>0"
		args = append(args, q)
	}
	if before > 0 {
		query += " AND u.id<?"
		args = append(args, before)
	}
	query += " ORDER BY u.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := h.service.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type choice struct {
		ID   uint64 `json:"id,string"`
		Name string `json:"name"`
	}
	items := []choice{}
	for rows.Next() {
		var c choice
		if err = rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = fmt.Sprint(items[limit-1].ID)
	}
	page := map[string]any{"items": items}
	if next != "" {
		page["next_before_id"] = next
	}
	return page, nil
}
