package docs_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	httpadapter "github.com/AJackTi/go-clean-architecture/internal/controller/http"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gin-gonic/gin"
	yaml "github.com/goccy/go-yaml"
)

//go:embed openapi.yaml
var openAPISource []byte

var httpMethods = map[string]struct{}{
	"delete":  {},
	"get":     {},
	"head":    {},
	"options": {},
	"patch":   {},
	"post":    {},
	"put":     {},
	"trace":   {},
}

const documentedRequestIDMaxBytes = 128

type openAPIDocument struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Security   []map[string][]string                 `json:"security"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Parameters      map[string]json.RawMessage `json:"parameters"`
		Responses       map[string]json.RawMessage `json:"responses"`
		Schemas         map[string]json.RawMessage `json:"schemas"`
		SecuritySchemes map[string]json.RawMessage `json:"securitySchemes"`
	} `json:"components"`
}

type operation struct {
	OperationID string                     `json:"operationId"`
	Responses   map[string]json.RawMessage `json:"responses"`
}

type schema struct {
	Ref                  string            `json:"$ref"`
	Type                 string            `json:"type"`
	Format               string            `json:"format"`
	Pattern              string            `json:"pattern"`
	NormalizedMinLength  *int              `json:"x-normalized-min-length"`
	NormalizedMaxLength  *int              `json:"x-normalized-max-length"`
	AdditionalProperties *bool             `json:"additionalProperties"`
	Required             []string          `json:"required"`
	Properties           map[string]schema `json:"properties"`
	Items                *schema           `json:"items"`
	MinLength            *int              `json:"minLength"`
	MaxLength            *int              `json:"maxLength"`
	Minimum              *int              `json:"minimum"`
	Maximum              *int              `json:"maximum"`
	Default              *int              `json:"default"`
	Const                string            `json:"const"`
	Enum                 []string          `json:"enum"`
}

type parameter struct {
	Name            string `json:"name"`
	In              string `json:"in"`
	Required        bool   `json:"required"`
	AllowEmptyValue bool   `json:"allowEmptyValue"`
	Schema          schema `json:"schema"`
}

func TestOpenAPIContractMatchesRouter(t *testing.T) {
	document, _ := loadOpenAPI(t)

	gin.SetMode(gin.TestMode)
	router := httpadapter.NewRouter(nil, nil)
	actual := make(map[string]struct{}, len(router.Routes()))
	for _, route := range router.Routes() {
		key := strings.ToLower(route.Method) + " " + toOpenAPIPath(route.Path)
		actual[key] = struct{}{}
	}

	documented := make(map[string]struct{})
	operationIDs := make(map[string]string)
	for path, pathItem := range document.Paths {
		for method, rawOperation := range pathItem {
			if _, isMethod := httpMethods[method]; !isMethod {
				continue
			}
			key := method + " " + path
			documented[key] = struct{}{}

			var value operation
			decodeJSON(t, rawOperation, &value)
			if value.OperationID == "" {
				t.Errorf("%s has no operationId", key)
			}
			if previous, exists := operationIDs[value.OperationID]; exists {
				t.Errorf("operationId %q is used by both %s and %s", value.OperationID, previous, key)
			}
			operationIDs[value.OperationID] = key
			if len(value.Responses) == 0 {
				t.Errorf("%s has no responses", key)
			}
		}
	}

	if diff := setDifference(actual, documented); len(diff) > 0 {
		t.Errorf("runtime routes missing from OpenAPI: %v", diff)
	}
	if diff := setDifference(documented, actual); len(diff) > 0 {
		t.Errorf("OpenAPI operations missing from runtime router: %v", diff)
	}
}

func TestOpenAPIContractDocumentsStableResponses(t *testing.T) {
	document, _ := loadOpenAPI(t)
	want := map[string][]string{
		"get /api/health":        {"200"},
		"get /api/healthz":       {"200", "503"},
		"post /api/v1/items":     {"200", "201", "400", "401", "404", "409", "422", "429", "500", "503"},
		"get /api/v1/items":      {"200", "400", "401", "429", "500", "503"},
		"get /api/v1/items/{id}": {"200", "400", "401", "404", "429", "500"},
	}

	for key, wantStatuses := range want {
		method, path, ok := strings.Cut(key, " ")
		if !ok {
			t.Fatalf("invalid test route key %q", key)
		}
		rawOperation, exists := document.Paths[path][method]
		if !exists {
			t.Errorf("OpenAPI operation %s is missing", key)
			continue
		}
		var value operation
		decodeJSON(t, rawOperation, &value)
		gotStatuses := sortedKeys(value.Responses)
		if strings.Join(gotStatuses, ",") != strings.Join(wantStatuses, ",") {
			t.Errorf("%s response statuses = %v, want %v", key, gotStatuses, wantStatuses)
		}
	}
}

func TestOpenAPIContractCapturesDomainAndTransportPolicy(t *testing.T) {
	document, _ := loadOpenAPI(t)

	item := componentSchema(t, document, "Item")
	assertClosedObject(t, "Item", item, "id", "name", "description", "created_at")
	assertStringPolicy(t, "Item.id", item.Properties["id"], 0, 0)
	if id := item.Properties["id"]; id.Format != "uuid" || id.Pattern == "" {
		t.Errorf("Item.id must document UUIDv4 format and pattern")
	}
	assertStringPolicy(t, "Item.name", item.Properties["name"], 1, 120)
	assertStringPolicy(t, "Item.description", item.Properties["description"], 0, 2000)
	if got := item.Properties["created_at"].Format; got != "date-time" {
		t.Errorf("Item.created_at format = %q, want date-time", got)
	}

	create := componentSchema(t, document, "CreateItemRequest")
	assertClosedObject(t, "CreateItemRequest", create, "name")
	assertNormalizedStringPolicy(t, "CreateItemRequest.name", create.Properties["name"], 1, 120)
	assertNormalizedStringPolicy(t, "CreateItemRequest.description", create.Properties["description"], 0, 2000)
	for _, serverOwned := range []string{"id", "created_at"} {
		if _, exists := create.Properties[serverOwned]; exists {
			t.Errorf("CreateItemRequest exposes server-owned field %q", serverOwned)
		}
	}

	page := componentSchema(t, document, "PageMeta")
	assertClosedObject(t, "PageMeta", page, "limit", "has_more")
	assertIntegerRange(t, "PageMeta.limit", page.Properties["limit"], 1, 100)
	assertIntegerMinimum(t, "PageMeta.offset", page.Properties["offset"], 0)
	if page.Properties["offset"].Ref != "" {
		t.Errorf("PageMeta.offset should be an inline optional integer, got ref %q", page.Properties["offset"].Ref)
	}
	cursor := componentSchema(t, document, "Cursor")
	assertStringPolicy(t, "Cursor", cursor, 4, 512)
	if cursor.Pattern == "" {
		t.Error("Cursor must document its versioned URL-safe pattern")
	}

	assertParameter(t, document, "Limit", "limit", "query", false, true, 0, 100, 20)
	assertParameter(t, document, "Offset", "offset", "query", false, true, 0, -1, 0)
	rawCursorParameter, exists := document.Components.Parameters["Cursor"]
	if !exists {
		t.Fatal("component parameter Cursor is missing")
	}
	var cursorParameter parameter
	decodeJSON(t, rawCursorParameter, &cursorParameter)
	if cursorParameter.Name != "cursor" || cursorParameter.In != "query" || cursorParameter.Required || cursorParameter.AllowEmptyValue || cursorParameter.Schema.Ref != "#/components/schemas/Cursor" {
		t.Errorf("Cursor parameter = %#v, want optional query reference", cursorParameter)
	}

	for name, status := range map[string]string{
		"LivenessResponse":             "ok",
		"ReadinessOKResponse":          "ok",
		"ReadinessUnavailableResponse": "unavailable",
	} {
		value := componentSchema(t, document, name)
		assertClosedObject(t, name, value, "status")
		if got := value.Properties["status"].Const; got != status {
			t.Errorf("%s.status const = %q, want %q", name, got, status)
		}
	}

	errorDetails := componentSchema(t, document, "ErrorDetails")
	assertClosedObject(t, "ErrorDetails", errorDetails, "code", "message")
	wantCodes := []string{"bad_request", "conflict", "cursor_unavailable", "idempotency_conflict", "idempotency_in_progress", "idempotency_unavailable", "internal_error", "invalid_cursor", "not_found", "rate_limited", "unauthorized", "validation_error"}
	gotCodes := append([]string(nil), errorDetails.Properties["code"].Enum...)
	sort.Strings(gotCodes)
	if strings.Join(gotCodes, ",") != strings.Join(wantCodes, ",") {
		t.Errorf("ErrorDetails.code enum = %v, want %v", gotCodes, wantCodes)
	}
}

func TestOpenAPIContractDocumentsIdempotentCreate(t *testing.T) {
	document, _ := loadOpenAPI(t)
	rawOperation, exists := document.Paths["/api/v1/items"]["post"]
	if !exists {
		t.Fatal("POST /api/v1/items is missing")
	}
	var operationValue struct {
		Parameters  []json.RawMessage          `json:"parameters"`
		Responses   map[string]json.RawMessage `json:"responses"`
		Idempotency map[string]any             `json:"x-idempotency"`
	}
	decodeJSON(t, rawOperation, &operationValue)
	if !hasReference(operationValue.Parameters, "#/components/parameters/IdempotencyKey") {
		t.Fatal("POST /api/v1/items must reference the optional Idempotency-Key parameter")
	}
	if operationValue.Idempotency["supported"] != true {
		t.Errorf("x-idempotency.supported = %#v, want true", operationValue.Idempotency["supported"])
	}
	if operationValue.Idempotency["retention"] != "24h after successful completion" {
		t.Errorf("x-idempotency.retention = %#v", operationValue.Idempotency["retention"])
	}

	keySchema := componentSchema(t, document, "IdempotencyKey")
	assertStringPolicy(t, "IdempotencyKey", keySchema, 1, 255)
	if keySchema.Pattern == "" {
		t.Error("IdempotencyKey must constrain values to an HTTP token pattern")
	}

	for _, status := range []string{"200", "201"} {
		response := resolveResponse(t, document, operationValue.Responses[status])
		rawHeaders, ok := response["headers"]
		if !ok {
			t.Fatalf("POST response %s has no headers", status)
		}
		var headers map[string]json.RawMessage
		decodeJSON(t, rawHeaders, &headers)
		if _, ok := headers["Idempotency-Key"]; !ok {
			t.Errorf("POST response %s has no Idempotency-Key header", status)
		}
		if status == "200" {
			var replayHeader struct {
				Ref string `json:"$ref"`
			}
			rawReplay, ok := headers["Idempotency-Replayed"]
			if !ok {
				t.Fatal("replay response must document Idempotency-Replayed")
			}
			decodeJSON(t, rawReplay, &replayHeader)
			if replayHeader.Ref != "#/components/headers/IdempotencyReplayed" {
				t.Errorf("Idempotency-Replayed ref = %q", replayHeader.Ref)
			}
		}
	}

	conflict := resolveResponse(t, document, operationValue.Responses["409"])
	var description string
	if rawDescription, ok := conflict["description"]; ok {
		decodeJSON(t, rawDescription, &description)
	}
	if !strings.Contains(description, "idempotency_conflict") || !strings.Contains(description, "idempotency_in_progress") {
		t.Errorf("409 description does not document idempotency outcomes: %q", description)
	}
	if _, ok := operationValue.Responses["503"]; !ok {
		t.Fatal("POST /api/v1/items must document 503 idempotency_unavailable")
	}
}

func TestOpenAPIContractDocumentsCursorPagination(t *testing.T) {
	document, _ := loadOpenAPI(t)
	rawOperation, exists := document.Paths["/api/v1/items"]["get"]
	if !exists {
		t.Fatal("GET /api/v1/items is missing")
	}
	var operationValue struct {
		Parameters []json.RawMessage          `json:"parameters"`
		Responses  map[string]json.RawMessage `json:"responses"`
	}
	decodeJSON(t, rawOperation, &operationValue)
	if !hasReference(operationValue.Parameters, "#/components/parameters/Cursor") {
		t.Fatal("GET /api/v1/items must reference the optional cursor parameter")
	}
	if _, ok := operationValue.Responses["503"]; !ok {
		t.Fatal("GET /api/v1/items must document cursor_unavailable")
	}
	meta := componentSchema(t, document, "PageMeta")
	if meta.Properties["next_cursor"].Ref != "#/components/schemas/Cursor" {
		t.Errorf("PageMeta.next_cursor ref = %q, want Cursor", meta.Properties["next_cursor"].Ref)
	}
}

func TestOpenAPIContractDocumentsOptionalBearerSecurity(t *testing.T) {
	document, _ := loadOpenAPI(t)
	rawScheme, exists := document.Components.SecuritySchemes["BearerAuth"]
	if !exists {
		t.Fatal("components.securitySchemes.BearerAuth is missing")
	}
	var scheme struct {
		Type         string `json:"type"`
		Scheme       string `json:"scheme"`
		BearerFormat string `json:"bearerFormat"`
	}
	decodeJSON(t, rawScheme, &scheme)
	if scheme.Type != "http" || scheme.Scheme != "bearer" || scheme.BearerFormat == "" {
		t.Errorf("BearerAuth scheme = %#v, want HTTP bearer scheme", scheme)
	}
	for _, path := range []string{"/api/v1/items", "/api/v1/items/{id}"} {
		var extension struct {
			Optional bool   `json:"optional"`
			Enabled  string `json:"enabled-by"`
		}
		rawPath := document.Paths[path]
		rawExtension, exists := rawPath["x-authentication"]
		if !exists {
			t.Errorf("%s has no x-authentication extension", path)
			continue
		}
		decodeJSON(t, rawExtension, &extension)
		if !extension.Optional || extension.Enabled != "AUTH_ENABLED" {
			t.Errorf("%s authentication extension = %#v", path, extension)
		}
	}
}

func TestOpenAPIContractUsesResolvableLocalReferences(t *testing.T) {
	_, raw := loadOpenAPI(t)
	walkReferences(t, raw, raw)
}

func TestOpenAPIContractDocumentsRequestIDEverywhere(t *testing.T) {
	document, _ := loadOpenAPI(t)
	if document.Security == nil || len(document.Security) != 0 {
		t.Fatalf("root security = %#v, want explicit empty security requirement", document.Security)
	}

	requestParameter, exists := document.Components.Parameters["RequestID"]
	if !exists {
		t.Fatal("components.parameters.RequestID is missing")
	}
	var parameterValue parameter
	decodeJSON(t, requestParameter, &parameterValue)
	if parameterValue.Name != "X-Request-ID" || parameterValue.In != "header" || parameterValue.Required {
		t.Errorf("RequestID parameter metadata = %#v, want optional X-Request-ID header", parameterValue)
	}
	if parameterValue.Schema.MaxLength == nil || *parameterValue.Schema.MaxLength != documentedRequestIDMaxBytes {
		t.Errorf("RequestID parameter maxLength = %v, want %d", parameterValue.Schema.MaxLength, documentedRequestIDMaxBytes)
	}
	if parameterValue.Schema.Pattern == "" {
		t.Error("RequestID parameter must constrain values to a safe token pattern")
	}

	for path, pathItem := range document.Paths {
		var parameters []json.RawMessage
		rawParameters, exists := pathItem["parameters"]
		if !exists {
			t.Errorf("%s has no path-level RequestID parameter", path)
		} else {
			decodeJSON(t, rawParameters, &parameters)
		}
		if !hasReference(parameters, "#/components/parameters/RequestID") {
			t.Errorf("%s does not reference components.parameters.RequestID", path)
		}

		for method, rawOperation := range pathItem {
			if _, isMethod := httpMethods[method]; !isMethod {
				continue
			}
			var value operation
			decodeJSON(t, rawOperation, &value)
			for status, rawResponse := range value.Responses {
				response := resolveResponse(t, document, rawResponse)
				rawHeaders, exists := response["headers"]
				if !exists {
					t.Errorf("%s %s response %s has no headers", method, path, status)
					continue
				}
				var headers map[string]json.RawMessage
				decodeJSON(t, rawHeaders, &headers)
				var requestHeader json.RawMessage
				for name, rawHeader := range headers {
					if strings.EqualFold(name, "X-Request-ID") {
						requestHeader = rawHeader
						break
					}
				}
				if len(requestHeader) == 0 {
					t.Errorf("%s %s response %s has no X-Request-ID header", method, path, status)
					continue
				}
				var headerRef struct {
					Ref string `json:"$ref"`
				}
				decodeJSON(t, requestHeader, &headerRef)
				if headerRef.Ref != "#/components/headers/RequestID" {
					t.Errorf("%s %s response %s X-Request-ID ref = %q, want reusable RequestID header", method, path, status, headerRef.Ref)
				}
			}
		}
	}
}

func TestOpenAPIConformsToOpenAPI31(t *testing.T) {
	loader := openapi3.NewLoader()
	document, err := loader.LoadFromData(openAPISource)
	if err != nil {
		t.Fatalf("load docs/openapi.yaml with kin-openapi: %v", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("docs/openapi.yaml does not conform to OpenAPI 3.1: %v", err)
	}
}

func loadOpenAPI(t *testing.T) (openAPIDocument, any) {
	t.Helper()

	converted, err := yaml.YAMLToJSON(openAPISource)
	if err != nil {
		t.Fatalf("parse docs/openapi.yaml: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(converted))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		t.Fatalf("decode docs/openapi.yaml as JSON: %v", err)
	}

	var document openAPIDocument
	decodeJSON(t, converted, &document)
	if document.OpenAPI != "3.1.0" {
		t.Errorf("openapi version = %q, want 3.1.0", document.OpenAPI)
	}
	if document.Info.Title == "" || document.Info.Version == "" {
		t.Errorf("OpenAPI info.title and info.version must be non-empty")
	}
	if len(document.Paths) == 0 || len(document.Components.Schemas) == 0 {
		t.Fatalf("OpenAPI document must define paths and component schemas")
	}
	return document, raw
}

func componentSchema(t *testing.T, document openAPIDocument, name string) schema {
	t.Helper()
	raw, exists := document.Components.Schemas[name]
	if !exists {
		t.Fatalf("component schema %q is missing", name)
	}
	var value schema
	decodeJSON(t, raw, &value)
	return value
}

func hasReference(values []json.RawMessage, want string) bool {
	for _, raw := range values {
		var value struct {
			Ref string `json:"$ref"`
		}
		if err := json.Unmarshal(raw, &value); err == nil && value.Ref == want {
			return true
		}
	}
	return false
}

func resolveResponse(t *testing.T, document openAPIDocument, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var value map[string]json.RawMessage
	decodeJSON(t, raw, &value)
	var reference string
	if rawReference, exists := value["$ref"]; exists {
		decodeJSON(t, rawReference, &reference)
		const prefix = "#/components/responses/"
		name := strings.TrimPrefix(reference, prefix)
		if name == reference {
			t.Fatalf("response reference %q is not a local component response", reference)
		}
		component, exists := document.Components.Responses[name]
		if !exists {
			t.Fatalf("response component %q is missing", name)
		}
		decodeJSON(t, component, &value)
	}
	return value
}

func assertClosedObject(t *testing.T, name string, value schema, required ...string) {
	t.Helper()
	if value.Type != "object" {
		t.Errorf("%s type = %q, want object", name, value.Type)
	}
	if value.AdditionalProperties == nil || *value.AdditionalProperties {
		t.Errorf("%s must set additionalProperties to false", name)
	}
	got := append([]string(nil), value.Required...)
	sort.Strings(got)
	want := append([]string(nil), required...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s required = %v, want %v", name, got, want)
	}
}

func assertStringPolicy(t *testing.T, name string, value schema, minLength, maxLength int) {
	t.Helper()
	if value.Type != "string" {
		t.Errorf("%s type = %q, want string", name, value.Type)
	}
	if minLength > 0 && (value.MinLength == nil || *value.MinLength != minLength) {
		t.Errorf("%s minLength must be %d", name, minLength)
	}
	if maxLength > 0 && (value.MaxLength == nil || *value.MaxLength != maxLength) {
		t.Errorf("%s maxLength must be %d", name, maxLength)
	}
}

func assertNormalizedStringPolicy(t *testing.T, name string, value schema, minimum, maximum int) {
	t.Helper()
	if value.Type != "string" {
		t.Errorf("%s type = %q, want string", name, value.Type)
	}
	if value.MinLength != nil || value.MaxLength != nil {
		t.Errorf("%s must not apply raw minLength/maxLength before trimming", name)
	}
	if minimum > 0 && (value.NormalizedMinLength == nil || *value.NormalizedMinLength != minimum) {
		t.Errorf("%s x-normalized-min-length must be %d", name, minimum)
	}
	if maximum > 0 && (value.NormalizedMaxLength == nil || *value.NormalizedMaxLength != maximum) {
		t.Errorf("%s x-normalized-max-length must be %d", name, maximum)
	}
}

func assertIntegerRange(t *testing.T, name string, value schema, minimum, maximum int) {
	t.Helper()
	assertIntegerMinimum(t, name, value, minimum)
	if value.Maximum == nil || *value.Maximum != maximum {
		t.Errorf("%s maximum must be %d", name, maximum)
	}
}

func assertIntegerMinimum(t *testing.T, name string, value schema, minimum int) {
	t.Helper()
	if value.Type != "integer" {
		t.Errorf("%s type = %q, want integer", name, value.Type)
	}
	if value.Minimum == nil || *value.Minimum != minimum {
		t.Errorf("%s minimum must be %d", name, minimum)
	}
}

func assertParameter(
	t *testing.T,
	document openAPIDocument,
	componentName string,
	name string,
	in string,
	required bool,
	allowEmpty bool,
	minimum int,
	maximum int,
	defaultValue int,
) {
	t.Helper()
	raw, exists := document.Components.Parameters[componentName]
	if !exists {
		t.Fatalf("component parameter %q is missing", componentName)
	}
	var value parameter
	decodeJSON(t, raw, &value)
	if value.Name != name || value.In != in || value.Required != required || value.AllowEmptyValue != allowEmpty {
		t.Errorf(
			"parameter %s metadata = {name:%q in:%q required:%t allowEmpty:%t}",
			componentName,
			value.Name,
			value.In,
			value.Required,
			value.AllowEmptyValue,
		)
	}
	assertIntegerMinimum(t, "parameter "+componentName, value.Schema, minimum)
	if maximum >= 0 && (value.Schema.Maximum == nil || *value.Schema.Maximum != maximum) {
		t.Errorf("parameter %s maximum must be %d", componentName, maximum)
	}
	if value.Schema.Default == nil || *value.Schema.Default != defaultValue {
		t.Errorf("parameter %s default must be %d", componentName, defaultValue)
	}
}

func walkReferences(t *testing.T, root any, current any) {
	t.Helper()
	switch value := current.(type) {
	case map[string]any:
		if reference, exists := value["$ref"]; exists {
			text, ok := reference.(string)
			if !ok || !strings.HasPrefix(text, "#/") {
				t.Errorf("OpenAPI reference must be local, got %#v", reference)
			} else if _, err := resolveJSONPointer(root, text); err != nil {
				t.Errorf("resolve OpenAPI reference %q: %v", text, err)
			}
		}
		for _, child := range value {
			walkReferences(t, root, child)
		}
	case []any:
		for _, child := range value {
			walkReferences(t, root, child)
		}
	}
}

func resolveJSONPointer(root any, reference string) (any, error) {
	current := root
	for _, rawToken := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(rawToken, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q does not point through an object", reference)
		}
		current, ok = object[token]
		if !ok {
			return nil, fmt.Errorf("token %q does not exist", token)
		}
	}
	return current, nil
}

func decodeJSON(t *testing.T, source []byte, destination any) {
	t.Helper()
	if err := json.Unmarshal(source, destination); err != nil {
		t.Fatalf("decode OpenAPI value: %v", err)
	}
}

func toOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[index] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	return strings.Join(parts, "/")
}

func setDifference(left, right map[string]struct{}) []string {
	var difference []string
	for value := range left {
		if _, exists := right[value]; !exists {
			difference = append(difference, value)
		}
	}
	sort.Strings(difference)
	return difference
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
