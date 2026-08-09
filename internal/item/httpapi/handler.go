// Package httpapi exposes the HTTP adapter for the item feature.
//
// The adapter deliberately knows only about the item service contract.  It is
// responsible for translating HTTP concerns (JSON, URL/query parsing and
// status codes) into that contract; business rules remain in package item.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const defaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// IdempotencyKeyHeader is the exact request/response header used by the
// create endpoint.
const IdempotencyKeyHeader = "Idempotency-Key"

// IdempotencyReplayedHeader is set to true when a successful response is a
// replay of an earlier create.
const IdempotencyReplayedHeader = "Idempotency-Replayed"

const idempotencyKeyHeader = IdempotencyKeyHeader

// Handler contains the HTTP endpoints for items.
type Handler struct {
	service      Service
	maxBodyBytes int64
}

// Service is the application boundary used by the HTTP adapter.  Keeping the
// interface here means the adapter can be tested with a small fake and does
// not couple HTTP code to a concrete persistence implementation.
type Service interface {
	Create(ctx context.Context, input item.CreateInput) (item.Item, error)
	Get(ctx context.Context, id uuid.UUID) (item.Item, error)
	List(ctx context.Context, params item.ListParams) (item.Page, error)
}

// New constructs an item HTTP handler.  A nil service is accepted so route
// registration remains side-effect free; requests then receive a sanitized
// 500 response instead of panicking.
func New(service Service, options ...Option) *Handler {
	h := &Handler{
		service:      service,
		maxBodyBytes: defaultMaxBodyBytes,
	}
	for _, option := range options {
		if option != nil {
			option(h)
		}
	}
	if h.maxBodyBytes <= 0 {
		h.maxBodyBytes = defaultMaxBodyBytes
	}
	return h
}

// RegisterRoutes mounts the item endpoints below group.  The group is
// expected to represent the API version (for example /api/v1).
func (h *Handler) RegisterRoutes(group *gin.RouterGroup) {
	if group == nil {
		return
	}
	routes := group.Group("/items")
	routes.POST("", h.Create)
	routes.GET("", h.List)
	routes.GET("/:id", h.Get)
}

// Mount is an explicit alias useful at composition roots.
func (h *Handler) Mount(group *gin.RouterGroup) { h.RegisterRoutes(group) }

// Create handles POST /items.
func (h *Handler) Create(c *gin.Context) {
	idempotencyKey, hasIdempotencyKey, err := parseIdempotencyKey(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "bad_request", "invalid idempotency key")
		return
	}
	var request *createRequest
	if err := decodeStrictJSON(c, &request, h.maxBodyBytes); err != nil {
		writeError(c, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}
	if request == nil {
		writeError(c, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	if h.service == nil {
		if hasIdempotencyKey {
			writeServiceError(c, item.ErrIdempotencyUnavailable)
			return
		}
		writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	input := item.CreateInput{
		Name:        request.Name,
		Description: request.Description,
	}
	var created item.Item
	replayed := false
	if hasIdempotencyKey {
		creator, ok := h.service.(item.IdempotentCreator)
		if !ok {
			writeServiceError(c, item.ErrIdempotencyUnavailable)
			return
		}
		created, replayed, err = creator.CreateIdempotent(c.Request.Context(), input, idempotencyKey)
	} else {
		created, err = h.service.Create(c.Request.Context(), input)
	}
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if created.ID == uuid.Nil {
		// A successful create must provide an addressable resource.  Treat a
		// malformed service result as an internal failure and do not emit a
		// misleading Location header.
		writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if hasIdempotencyKey {
		// Echo only after a successful atomic outcome. Error responses do not
		// reflect potentially sensitive client tokens.
		c.Header(idempotencyKeyHeader, idempotencyKey)
	}

	location := resourceLocation(c, created.ID)
	c.Header("Location", location)
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
		c.Header(IdempotencyReplayedHeader, "true")
	}
	c.JSON(status, dataResponse[item.Item]{Data: created})
}

// Get handles GET /items/:id.
func (h *Handler) Get(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil || id == uuid.Nil {
		writeError(c, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if h.service == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	found, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, dataResponse[item.Item]{Data: found})
}

// List handles GET /items?limit=<n>&offset=<n>.
func (h *Handler) List(c *gin.Context) {
	params, err := parseListParams(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "bad_request", "invalid pagination parameters")
		return
	}
	if h.service == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	page, err := h.service.List(c.Request.Context(), params)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	items := page.Items
	if items == nil {
		items = []item.Item{}
	}
	c.JSON(http.StatusOK, listResponse{
		Data: items,
		Meta: pageMeta{
			Limit:   page.Limit,
			Offset:  page.Offset,
			HasMore: page.HasMore,
		},
	})
}

type createRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type dataResponse[T any] struct {
	Data T `json:"data"`
}

type listResponse struct {
	Data []item.Item `json:"data"`
	Meta pageMeta    `json:"meta"`
}

type pageMeta struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

type errorResponse struct {
	Error errorDetails `json:"error"`
}

type errorDetails struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, item.ErrIdempotencyConflict):
		writeError(c, http.StatusConflict, "idempotency_conflict", "idempotency key was used with a different request")
	case errors.Is(err, item.ErrIdempotencyInProgress):
		c.Header("Retry-After", "1")
		writeError(c, http.StatusConflict, "idempotency_in_progress", "idempotency request is still in progress")
	case errors.Is(err, item.ErrIdempotencyUnavailable), errors.Is(err, item.ErrIdempotencyState):
		writeError(c, http.StatusServiceUnavailable, "idempotency_unavailable", "idempotent create is temporarily unavailable")
	case errors.Is(err, item.ErrInvalidInput):
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "invalid item")
	case errors.Is(err, item.ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", "item not found")
	case errors.Is(err, item.ErrConflict):
		writeError(c, http.StatusConflict, "conflict", "item already exists")
	default:
		// Never expose persistence/provider errors to API clients.
		writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorResponse{Error: errorDetails{Code: code, Message: message}})
}

func resourceLocation(c *gin.Context, id uuid.UUID) string {
	path := c.Request.URL.Path
	if path == "" {
		path = "/items"
	}
	path = strings.TrimRight(path, "/")
	return path + "/" + url.PathEscape(id.String())
}

func decodeStrictJSON(c *gin.Context, destination any, maxBytes int64) error {
	if c.Request == nil || c.Request.Body == nil {
		return io.ErrUnexpectedEOF
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBodyBytes
	}
	if c.Request.ContentLength > maxBytes {
		return &http.MaxBytesError{Limit: maxBytes}
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	// Decode a second value to reject concatenated JSON documents.  Whitespace
	// is accepted because the second decode then returns io.EOF.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errTrailingJSON
		}
		return err
	}
	return nil
}

var errTrailingJSON = errors.New("trailing JSON value")

func parseListParams(c *gin.Context) (item.ListParams, error) {
	values := c.Request.URL.Query()
	limit, err := parseQueryInt(values, "limit", item.DefaultPageSize)
	if err != nil {
		return item.ListParams{}, errInvalidPagination
	}
	// An explicit zero is a convenient way for clients to request the server
	// default.  Negative values and values above the configured maximum remain
	// invalid rather than being silently normalized.
	if limit == 0 {
		limit = item.DefaultPageSize
	}
	if limit < 0 || limit > item.MaxPageSize {
		return item.ListParams{}, errInvalidPagination
	}
	offset, err := parseQueryInt(values, "offset", 0)
	if err != nil || offset < 0 {
		return item.ListParams{}, errInvalidPagination
	}
	return item.ListParams{Limit: limit, Offset: offset}, nil
}

func parseQueryInt(values url.Values, key string, fallback int) (int, error) {
	entries, ok := values[key]
	if !ok || len(entries) == 0 || entries[0] == "" {
		return fallback, nil
	}
	if len(entries) != 1 {
		return 0, errInvalidPagination
	}
	parsed, err := strconv.Atoi(entries[0])
	if err != nil {
		return 0, errInvalidPagination
	}
	return parsed, nil
}

var errInvalidPagination = errors.New("invalid pagination")

func parseIdempotencyKey(c *gin.Context) (string, bool, error) {
	if c == nil || c.Request == nil {
		return "", false, nil
	}
	var values []string
	for headerName, headerValues := range c.Request.Header {
		if strings.EqualFold(headerName, idempotencyKeyHeader) {
			values = append(values, headerValues...)
		}
	}
	if len(values) == 0 {
		return "", false, nil
	}
	if len(values) != 1 {
		return "", true, &item.ValidationError{Field: "idempotency_key", Reason: "must contain exactly one value"}
	}
	if err := item.ValidateIdempotencyKey(values[0]); err != nil {
		return "", true, err
	}
	return values[0], true, nil
}
