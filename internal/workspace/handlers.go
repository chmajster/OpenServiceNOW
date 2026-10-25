package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	incidentdomain "github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type PermissionProvider interface { Permissions(context.Context,identity.SessionIdentity)([]string,error) }

type Handler struct { service *Service; permissions PermissionProvider }

func NewHandler(service *Service,permissions PermissionProvider)*Handler{return &Handler{service:service,permissions:permissions}}

type actorContextKey struct{}
func actorFromRequest(r *http.Request)Actor { actor,_:=r.Context().Value(actorContextKey{}).(Actor);return actor }

func RegisterRoutes(router chi.Router,handler *Handler,auth *identity.Middleware) {
	router.Route("/api/v1/workspace",func(r chi.Router) {
		r.Use(auth.Authenticate);r.Use(handler.context)
		r.Get("/fields",handler.Fields)
		r.Get("/references/{kind}",handler.References)
		r.Post("/queue",handler.Query)
		r.Get("/items/{type}/{id}",handler.Get)
		r.Get("/views",handler.ListViews)
		r.With(auth.RequireCSRF).Post("/views",handler.SaveView)
		r.With(auth.RequireCSRF).Put("/views/{id}",handler.SaveView)
		r.With(auth.RequireCSRF).Delete("/views/{id}",handler.DeleteView)
		r.With(auth.RequireCSRF).Post("/actions",handler.Action)
		r.With(auth.RequireCSRF).Post("/exports",handler.CreateExport)
		r.Get("/exports/{id}",handler.ExportStatus)
		r.Get("/exports/{id}/file",handler.ExportFile)
	})
}

func (h *Handler) context(next http.Handler)http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
		w.Header().Set("Cache-Control","no-store")
		session,ok:=identity.FromContext(r.Context());if !ok{writeError(w,r,ErrForbidden);return}
		permissions,err:=h.permissions.Permissions(r.Context(),session);if err!=nil{writeError(w,r,err);return}
		actor:=Actor{TenantID:session.TenantID,UserID:session.UserID,RequestID:middleware.GetReqID(r.Context()),Permissions:map[string]bool{}}
		for _,permission:=range permissions{actor.Permissions[permission]=true}
		next.ServeHTTP(w,r.WithContext(context.WithValue(r.Context(),actorContextKey{},actor)))
	})
}

func (h *Handler) Fields(w http.ResponseWriter,r *http.Request) {
	actor:=actorFromRequest(r)
	if !actor.CanUse(){writeError(w,r,ErrForbidden);return}
	fields,err:=h.service.ConfiguredFields(r.Context(),actor);if err!=nil{writeError(w,r,err);return}
	httpapi.WriteJSON(w,http.StatusOK,map[string]any{"fields":fields,"types":[]map[string]string{{"key":IncidentType,"label":"Incydent"},{"key":TaskType,"label":"Zadanie realizacji"}}})
}

func (h *Handler) References(w http.ResponseWriter,r *http.Request) {
	after,err:=parseOptionalID(r.URL.Query().Get("after_id"));if err!=nil{writeError(w,r,err);return}
	limit:=25;if raw:=r.URL.Query().Get("limit");raw!="" {limit,err=strconv.Atoi(raw);if err!=nil{writeError(w,r,ErrInvalid);return}}
	page,err:=h.service.References(r.Context(),actorFromRequest(r),chi.URLParam(r,"kind"),r.URL.Query().Get("q"),after,limit);respondJSON(w,r,http.StatusOK,page,err)
}

func (h *Handler) Query(w http.ResponseWriter,r *http.Request) {
	var input Query;if !decodeJSON(w,r,&input){return}
	page,err:=h.service.Query(r.Context(),actorFromRequest(r),input);respondJSON(w,r,http.StatusOK,page,err)
}

func (h *Handler) Get(w http.ResponseWriter,r *http.Request) {
	item,err:=h.service.Get(r.Context(),actorFromRequest(r),chi.URLParam(r,"type"),chi.URLParam(r,"id"));respondJSON(w,r,http.StatusOK,item,err)
}

func (h *Handler) ListViews(w http.ResponseWriter,r *http.Request) {
	views,err:=h.service.ListViews(r.Context(),actorFromRequest(r));respondJSON(w,r,http.StatusOK,map[string]any{"items":views},err)
}

func (h *Handler) SaveView(w http.ResponseWriter,r *http.Request) {
	var input SaveViewInput;if !decodeJSON(w,r,&input){return}
	var id uint64
	if r.Method==http.MethodPut {var err error;id,err=parseID(chi.URLParam(r,"id"));if err!=nil{writeError(w,r,err);return}}
	item,err:=h.service.SaveView(r.Context(),actorFromRequest(r),id,input);respondJSON(w,r,http.StatusOK,item,err)
}

func (h *Handler) DeleteView(w http.ResponseWriter,r *http.Request) {
	id,err:=parseID(chi.URLParam(r,"id"));if err!=nil{writeError(w,r,err);return}
	version,err:=parseID(r.URL.Query().Get("version"));if err!=nil{writeError(w,r,err);return}
	if err=h.service.DeleteView(r.Context(),actorFromRequest(r),id,version);err!=nil{writeError(w,r,err);return};w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Action(w http.ResponseWriter,r *http.Request) {
	var input ActionInput;if !decodeJSON(w,r,&input){return}
	result,err:=h.service.Action(r.Context(),actorFromRequest(r),input);respondJSON(w,r,http.StatusOK,result,err)
}

func (h *Handler) CreateExport(w http.ResponseWriter,r *http.Request) {
	var input Query;if !decodeJSON(w,r,&input){return}
	status,err:=h.service.CreateExport(r.Context(),actorFromRequest(r),input,r.Header.Get("Idempotency-Key"));respondJSON(w,r,http.StatusAccepted,status,err)
}

func (h *Handler) ExportStatus(w http.ResponseWriter,r *http.Request) {
	id,err:=parseID(chi.URLParam(r,"id"));if err!=nil{writeError(w,r,err);return}
	status,err:=h.service.ExportStatus(r.Context(),actorFromRequest(r),id);respondJSON(w,r,http.StatusOK,status,err)
}

func (h *Handler) ExportFile(w http.ResponseWriter,r *http.Request) {
	id,err:=parseID(chi.URLParam(r,"id"));if err!=nil{writeError(w,r,err);return}
	actor:=actorFromRequest(r);attachmentID,err:=h.service.CanDownloadExport(r.Context(),actor,id);if err!=nil{writeError(w,r,err);return}
	attachment,reader,err:=h.service.files.Open(r.Context(),actor.TenantID,attachmentID);if err!=nil{writeError(w,r,err);return};defer reader.Close()
	if attachment.ObjectType!="workspace_export"||attachment.ObjectID!=strconv.FormatUint(id,10){writeError(w,r,ErrNotFound);return}
	data,err:=io.ReadAll(io.LimitReader(reader,attachments.MaxFileBytes+1));if err!=nil{writeError(w,r,err);return};if int64(len(data))>attachments.MaxFileBytes{writeError(w,r,attachments.ErrTooLarge);return}
	w.Header().Set("Content-Type","text/csv; charset=utf-8");w.Header().Set("Content-Disposition",fmt.Sprintf("attachment; filename=\"work-items-%d.csv\"",id));w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Cache-Control","no-store")
	w.WriteHeader(http.StatusOK);_,_=w.Write(data)
}

func decodeJSON(w http.ResponseWriter,r *http.Request,value any)bool {
	if err:=httpapi.DecodeJSON(r,value);err!=nil{httpapi.WriteDecodeError(w,r,err);return false};return true
}

func respondJSON(w http.ResponseWriter,r *http.Request,status int,value any,err error) {
	if err!=nil{writeError(w,r,err);return};httpapi.WriteJSON(w,status,value)
}

func writeError(w http.ResponseWriter,r *http.Request,err error) {
	if httpapi.WriteContextError(w,r,err){return}
	status,code,message:=http.StatusInternalServerError,"WORKSPACE_FAILED","Nie udało się wykonać operacji workspace."
	switch {
	case errors.Is(err,ErrInvalid),errors.Is(err,attachments.ErrInvalid),errors.Is(err,jobs.ErrInvalid):status,code,message=http.StatusBadRequest,"INVALID_WORKSPACE_INPUT",err.Error()
	case errors.Is(err,ErrNotFound),errors.Is(err,incidentdomain.ErrNotFound),errors.Is(err,requests.ErrNotFound),errors.Is(err,workrecords.ErrNotFound),errors.Is(err,jobs.ErrNotFound),errors.Is(err,attachments.ErrNotFound):status,code,message=http.StatusNotFound,"NOT_FOUND","Nie znaleziono dostępnego rekordu."
	case errors.Is(err,ErrForbidden),errors.Is(err,incidentdomain.ErrForbidden),errors.Is(err,requests.ErrForbidden),errors.Is(err,workrecords.ErrForbidden):status,code,message=http.StatusForbidden,"FORBIDDEN","Brak uprawnień do operacji."
	case errors.Is(err,ErrConflict),errors.Is(err,incidentdomain.ErrConflict),errors.Is(err,requests.ErrConflict),errors.Is(err,workrecords.ErrConflict):status,code,message=http.StatusConflict,"VERSION_CONFLICT",err.Error()
	case errors.Is(err,attachments.ErrTooLarge):status,code,message=http.StatusRequestEntityTooLarge,"EXPORT_TOO_LARGE","Eksport przekracza dozwolony rozmiar. Zawęź filtr."
	case errors.Is(err,database.ErrCommitUncertain):status,code,message=http.StatusServiceUnavailable,"COMMIT_UNCERTAIN","Nie potwierdzono zapisu. Sprawdź stan operacji przed ponowieniem."
	case strings.Contains(strings.ToLower(err.Error()),"duplicate entry"):status,code,message=http.StatusConflict,"VIEW_NAME_CONFLICT","Widok o tej nazwie już istnieje."
	}
	httpapi.WriteError(w,r,status,code,message)
}

func parseID(raw string)(uint64,error) {id,err:=strconv.ParseUint(raw,10,64);if err!=nil||id==0{return 0,ErrInvalid};return id,nil}
func parseOptionalID(raw string)(uint64,error) {if raw==""{return 0,nil};return parseID(raw)}

