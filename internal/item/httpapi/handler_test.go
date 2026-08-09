package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/AJackTi/go-clean-architecture/internal/item/httpapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestCreateReturnsCreatedItemAndLocation(t *testing.T) {
	t.Parallel()

	want := item.Item{
		ID:          uuid.MustParse("c80c1043-a6cd-42bf-984e-0191352f4b26"),
		Name:        "Mechanical keyboard",
		Description: "Hot-swappable",
		CreatedAt:   time.Date(2026, time.August, 9, 12, 30, 0, 0, time.UTC),
	}
	var gotInput item.CreateInput
	service := &serviceStub{
		create: func(_ context.Context, input item.CreateInput) (item.Item, error) {
			gotInput = input
			return want, nil
		},
	}
	router := newRouter(service)

	response := performRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/items",
		`{"name":"Mechanical keyboard","description":"Hot-swappable"}`,
	)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if got := response.Header().Get("Location"); got != "/api/v1/items/"+want.ID.String() {
		t.Errorf("Location = %q, want %q", got, "/api/v1/items/"+want.ID.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if gotInput != (item.CreateInput{Name: want.Name, Description: want.Description}) {
		t.Errorf("Create input = %#v, want name and description from request", gotInput)
	}

	var body struct {
		Data item.Item `json:"data"`
	}
	decodeResponse(t, response, &body)
	if body.Data != want {
		t.Errorf("response item = %#v, want %#v", body.Data, want)
	}
}

func TestCreateRejectsNonStrictJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "null body", body: "null"},
		{name: "malformed document", body: `{"name":`},
		{name: "unknown field", body: `{"name":"keyboard","unexpected":true}`},
		{name: "trailing object", body: `{"name":"keyboard"}{}`},
		{name: "trailing primitive", body: `{"name":"keyboard"} true`},
		{name: "wrong JSON shape", body: `[{"name":"keyboard"}]`},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			service := &serviceStub{
				create: func(context.Context, item.CreateInput) (item.Item, error) {
					calls++
					return item.Item{}, nil
				},
			}
			response := performRequest(t, newRouter(service), http.MethodPost, "/api/v1/items", test.body)

			assertAPIError(t, response, http.StatusBadRequest, "bad_request", "invalid request body")
			if calls != 0 {
				t.Errorf("service called %d times, want 0", calls)
			}
		})
	}
}

func TestCreateEnforcesBodySizeForKnownAndStreamingLengths(t *testing.T) {
	t.Parallel()

	for _, contentLength := range []int64{int64(len(`{"name":"body larger than limit"}`)), -1} {
		contentLength := contentLength
		t.Run(fmt.Sprintf("content-length-%d", contentLength), func(t *testing.T) {
			t.Parallel()

			calls := 0
			service := &serviceStub{
				create: func(context.Context, item.CreateInput) (item.Item, error) {
					calls++
					return item.Item{}, nil
				},
			}
			router := gin.New()
			httpapi.New(service, httpapi.WithMaxBodyBytes(16)).RegisterRoutes(router.Group("/api/v1"))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/items", nil)
			request.Body = io.NopCloser(strings.NewReader(`{"name":"body larger than limit"}`))
			request.ContentLength = contentLength
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertAPIError(t, response, http.StatusBadRequest, "bad_request", "invalid request body")
			if calls != 0 {
				t.Errorf("service called %d times, want 0", calls)
			}
		})
	}
}

func TestCreateMapsServiceErrorsToStableResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		status     int
		code       string
		message    string
		mustNotSee string
	}{
		{
			name:    "validation",
			err:     fmt.Errorf("name is empty: %w", item.ErrInvalidInput),
			status:  http.StatusUnprocessableEntity,
			code:    "validation_error",
			message: "invalid item",
		},
		{
			name:    "not found",
			err:     fmt.Errorf("wrapped: %w", item.ErrNotFound),
			status:  http.StatusNotFound,
			code:    "not_found",
			message: "item not found",
		},
		{
			name:    "conflict",
			err:     fmt.Errorf("wrapped: %w", item.ErrConflict),
			status:  http.StatusConflict,
			code:    "conflict",
			message: "item already exists",
		},
		{
			name:       "internal error is sanitized",
			err:        errors.New("postgres password=super-secret"),
			status:     http.StatusInternalServerError,
			code:       "internal_error",
			message:    "internal server error",
			mustNotSee: "super-secret",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &serviceStub{
				create: func(context.Context, item.CreateInput) (item.Item, error) {
					return item.Item{}, test.err
				},
			}
			response := performRequest(
				t,
				newRouter(service),
				http.MethodPost,
				"/api/v1/items",
				`{"name":"keyboard","description":"quiet"}`,
			)

			assertAPIError(t, response, test.status, test.code, test.message)
			if test.mustNotSee != "" && strings.Contains(response.Body.String(), test.mustNotSee) {
				t.Errorf("response leaks internal detail %q: %s", test.mustNotSee, response.Body.String())
			}
		})
	}
}

func TestCreateRejectsServiceResultWithoutID(t *testing.T) {
	t.Parallel()

	service := &serviceStub{
		create: func(context.Context, item.CreateInput) (item.Item, error) {
			return item.Item{Name: "keyboard"}, nil
		},
	}
	response := performRequest(
		t,
		newRouter(service),
		http.MethodPost,
		"/api/v1/items",
		`{"name":"keyboard"}`,
	)

	assertAPIError(t, response, http.StatusInternalServerError, "internal_error", "internal server error")
	if got := response.Header().Get("Location"); got != "" {
		t.Errorf("Location = %q, want no header on failure", got)
	}
}

func TestGetReturnsItemByUUID(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("0f3f84c5-64b7-4df5-a6a6-fe07f97a85ed")
	want := item.Item{ID: id, Name: "keyboard", Description: "quiet"}
	var gotID uuid.UUID
	service := &serviceStub{
		get: func(_ context.Context, requestedID uuid.UUID) (item.Item, error) {
			gotID = requestedID
			return want, nil
		},
	}

	response := performRequest(t, newRouter(service), http.MethodGet, "/api/v1/items/"+id.String(), "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if gotID != id {
		t.Errorf("Get id = %s, want %s", gotID, id)
	}
	var body struct {
		Data item.Item `json:"data"`
	}
	decodeResponse(t, response, &body)
	if body.Data != want {
		t.Errorf("response item = %#v, want %#v", body.Data, want)
	}
}

func TestGetRejectsInvalidUUIDBeforeCallingService(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"not-a-uuid", uuid.Nil.String()} {
		id := id
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			calls := 0
			service := &serviceStub{
				get: func(context.Context, uuid.UUID) (item.Item, error) {
					calls++
					return item.Item{}, nil
				},
			}
			response := performRequest(t, newRouter(service), http.MethodGet, "/api/v1/items/"+id, "")

			assertAPIError(t, response, http.StatusBadRequest, "bad_request", "invalid item id")
			if calls != 0 {
				t.Errorf("service called %d times, want 0", calls)
			}
		})
	}
}

func TestGetMapsNotFound(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("59df8009-a000-46a2-97bc-d37c7a17d354")
	service := &serviceStub{
		get: func(context.Context, uuid.UUID) (item.Item, error) {
			return item.Item{}, item.ErrNotFound
		},
	}
	response := performRequest(t, newRouter(service), http.MethodGet, "/api/v1/items/"+id.String(), "")

	assertAPIError(t, response, http.StatusNotFound, "not_found", "item not found")
}

func TestListReturnsDataAndPaginationMetadata(t *testing.T) {
	t.Parallel()

	wantItems := []item.Item{
		{ID: uuid.MustParse("15709605-fde8-49ce-b867-c9af294f2bf5"), Name: "keyboard"},
		{ID: uuid.MustParse("09c5ec30-5908-4398-9df7-d0c14dd8e1d1"), Name: "mouse"},
	}
	var gotParams item.ListParams
	service := &serviceStub{
		list: func(_ context.Context, params item.ListParams) (item.Page, error) {
			gotParams = params
			return item.Page{Items: wantItems, Limit: 2, Offset: 4, HasMore: true}, nil
		},
	}
	response := performRequest(t, newRouter(service), http.MethodGet, "/api/v1/items?limit=2&offset=4", "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if gotParams != (item.ListParams{Limit: 2, Offset: 4}) {
		t.Errorf("List params = %#v, want limit=2 offset=4", gotParams)
	}
	var body struct {
		Data []item.Item `json:"data"`
		Meta struct {
			Limit   int  `json:"limit"`
			Offset  int  `json:"offset"`
			HasMore bool `json:"has_more"`
		} `json:"meta"`
	}
	decodeResponse(t, response, &body)
	if !itemsEqual(body.Data, wantItems) {
		t.Errorf("response data = %#v, want %#v", body.Data, wantItems)
	}
	if body.Meta.Limit != 2 || body.Meta.Offset != 4 || !body.Meta.HasMore {
		t.Errorf("response meta = %#v, want limit=2 offset=4 has_more=true", body.Meta)
	}
}

func TestListUsesDefaultLimitAndAlwaysReturnsAnArray(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"/api/v1/items", "/api/v1/items?limit=0", "/api/v1/items?limit=&offset="} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			var got item.ListParams
			service := &serviceStub{
				list: func(_ context.Context, params item.ListParams) (item.Page, error) {
					got = params
					return item.Page{Items: nil, Limit: 20, Offset: 0}, nil
				},
			}
			response := performRequest(t, newRouter(service), http.MethodGet, target, "")

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
			}
			if got != (item.ListParams{Limit: 20, Offset: 0}) {
				t.Errorf("List params = %#v, want default limit=20 offset=0", got)
			}
			var raw struct {
				Data json.RawMessage `json:"data"`
				Meta json.RawMessage `json:"meta"`
			}
			decodeResponse(t, response, &raw)
			if string(raw.Data) != "[]" {
				t.Errorf("data JSON = %s, want []", raw.Data)
			}
		})
	}
}

func TestListRejectsInvalidPaginationBeforeCallingService(t *testing.T) {
	t.Parallel()

	targets := []string{
		"/api/v1/items?limit=-1",
		"/api/v1/items?limit=101",
		"/api/v1/items?limit=abc",
		"/api/v1/items?limit=1&limit=2",
		"/api/v1/items?offset=-1",
		"/api/v1/items?offset=abc",
		"/api/v1/items?offset=1&offset=2",
		"/api/v1/items?limit=999999999999999999999999999999",
	}
	for _, target := range targets {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			calls := 0
			service := &serviceStub{
				list: func(context.Context, item.ListParams) (item.Page, error) {
					calls++
					return item.Page{}, nil
				},
			}
			response := performRequest(t, newRouter(service), http.MethodGet, target, "")

			assertAPIError(
				t,
				response,
				http.StatusBadRequest,
				"bad_request",
				"invalid pagination parameters",
			)
			if calls != 0 {
				t.Errorf("service called %d times, want 0", calls)
			}
		})
	}
}

func TestNilServiceReturnsSanitizedInternalError(t *testing.T) {
	t.Parallel()

	router := newRouter(nil)
	response := performRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/items",
		`{"name":"keyboard"}`,
	)

	assertAPIError(t, response, http.StatusInternalServerError, "internal_error", "internal server error")
}

type serviceStub struct {
	create func(context.Context, item.CreateInput) (item.Item, error)
	get    func(context.Context, uuid.UUID) (item.Item, error)
	list   func(context.Context, item.ListParams) (item.Page, error)
}

func (s *serviceStub) Create(ctx context.Context, input item.CreateInput) (item.Item, error) {
	if s.create == nil {
		return item.Item{}, errors.New("unexpected Create call")
	}
	return s.create(ctx, input)
}

func (s *serviceStub) Get(ctx context.Context, id uuid.UUID) (item.Item, error) {
	if s.get == nil {
		return item.Item{}, errors.New("unexpected Get call")
	}
	return s.get(ctx, id)
}

func (s *serviceStub) List(ctx context.Context, params item.ListParams) (item.Page, error) {
	if s.list == nil {
		return item.Page{}, errors.New("unexpected List call")
	}
	return s.list(ctx, params)
}

func newRouter(service httpapi.Service) *gin.Engine {
	router := gin.New()
	httpapi.New(service).RegisterRoutes(router.Group("/api/v1"))
	return router
}

func performRequest(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()

	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("response contains trailing JSON: %v", err)
	}
}

func assertAPIError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
	wantCode string,
	wantMessage string,
) {
	t.Helper()

	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, wantStatus, response.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeResponse(t, response, &body)
	if body.Error.Code != wantCode || body.Error.Message != wantMessage {
		t.Errorf(
			"error = {code:%q message:%q}, want {code:%q message:%q}",
			body.Error.Code,
			body.Error.Message,
			wantCode,
			wantMessage,
		)
	}
}

func itemsEqual(left, right []item.Item) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
