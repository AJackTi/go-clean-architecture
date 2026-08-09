package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testValue = "fixture-value-0123456789"

func TestParseBearerHeaderAcceptsStrictRFC6750Values(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "canonical", header: "Bearer abcDEF012-._~+/", want: "abcDEF012-._~+/"},
		{name: "case insensitive scheme", header: "bEaReR opaque-token", want: "opaque-token"},
		{name: "base64 padding", header: "Bearer YWJjZA==", want: "YWJjZA=="},
		{name: "JWT shape", header: "Bearer eyJhbGciOiJub25lIn0.eyJzdWIiOiJhIn0.signature", want: "eyJhbGciOiJub25lIn0.eyJzdWIiOiJhIn0.signature"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseBearerHeader([]string{test.header})
			if err != nil {
				t.Fatalf("ParseBearerHeader returned error: %v", err)
			}
			if got != test.want {
				t.Errorf("token = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseBearerHeaderRejectsMalformedAndDuplicateValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		want   error
	}{
		{name: "nil values", values: nil, want: ErrMissingCredentials},
		{name: "empty values", values: []string{}, want: ErrMissingCredentials},
		{name: "empty value", values: []string{""}, want: ErrInvalidCredentials},
		{name: "duplicate valid", values: []string{"Bearer first", "Bearer second"}, want: ErrDuplicateCredentials},
		{name: "duplicate empty", values: []string{"", ""}, want: ErrDuplicateCredentials},
		{name: "unsupported scheme", values: []string{"Basic opaque"}, want: ErrUnsupportedScheme},
		{name: "missing separator", values: []string{"Bearer"}, want: ErrInvalidCredentials},
		{name: "missing token", values: []string{"Bearer "}, want: ErrInvalidCredentials},
		{name: "leading space", values: []string{" Bearer token"}, want: ErrInvalidCredentials},
		{name: "trailing space", values: []string{"Bearer token "}, want: ErrInvalidCredentials},
		{name: "repeated space", values: []string{"Bearer  token"}, want: ErrInvalidCredentials},
		{name: "tab separator", values: []string{"Bearer\ttoken"}, want: ErrInvalidCredentials},
		{name: "embedded newline", values: []string{"Bearer token\nvalue"}, want: ErrInvalidCredentials},
		{name: "comma-delimited values", values: []string{"Bearer first,Bearer second"}, want: ErrInvalidCredentials},
		{name: "interior padding", values: []string{"Bearer abc=def"}, want: ErrInvalidCredentials},
		{name: "padding only", values: []string{"Bearer ==="}, want: ErrInvalidCredentials},
		{name: "quote", values: []string{`Bearer "token"`}, want: ErrInvalidCredentials},
		{name: "backslash", values: []string{`Bearer token\\value`}, want: ErrInvalidCredentials},
		{name: "non ASCII", values: []string{"Bearer tøkén"}, want: ErrInvalidCredentials},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			token, err := ParseBearerHeader(test.values)
			if token != "" {
				t.Errorf("rejected token = %q, want empty", token)
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
			if !errors.Is(test.want, ErrMissingCredentials) && !errors.Is(test.want, ErrDuplicateCredentials) &&
				!errors.Is(test.want, ErrUnsupportedScheme) && !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("error = %v, want common ErrInvalidCredentials sentinel", err)
			}
		})
	}
}

func TestParseBearerHeaderEnforcesTokenByteBoundary(t *testing.T) {
	t.Parallel()

	want := strings.Repeat("a", MaxBearerTokenBytes)
	got, err := ParseBearerHeader([]string{"Bearer " + want})
	if err != nil {
		t.Fatalf("maximum token: %v", err)
	}
	if got != want {
		t.Fatal("maximum token was not returned intact")
	}

	got, err = ParseBearerHeader([]string{"Bearer " + want + "a"})
	if got != "" {
		t.Error("oversized token was returned")
	}
	if !errors.Is(err, ErrCredentialsTooLong) || !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("oversized token error = %v, want ErrCredentialsTooLong and ErrInvalidCredentials", err)
	}
}

func FuzzParseBearerHeaderNeverPanics(f *testing.F) {
	f.Add("Bearer fixture-value")
	f.Add("Basic fixture-value")
	f.Add("")
	f.Add("Bearer " + strings.Repeat("a", MaxBearerTokenBytes+1))
	f.Add("Bearer\ttab")

	f.Fuzz(func(t *testing.T, value string) {
		token, err := ParseBearerHeader([]string{value})
		if err != nil {
			if token != "" {
				t.Fatalf("rejected token = %q, want empty", token)
			}
			return
		}
		if len(token) == 0 || len(token) > MaxBearerTokenBytes || !validBearerToken(token) {
			t.Fatalf("accepted token violates parser bounds: length=%d", len(token))
		}
	})
}

func TestParseBearerTokenReadsExactlyOneAuthorizationHeader(t *testing.T) {
	t.Parallel()

	if _, err := ParseBearerToken(nil); !errors.Is(err, ErrNilRequest) {
		t.Fatalf("nil request error = %v, want ErrNilRequest", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := ParseBearerToken(request); !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("missing header error = %v, want ErrMissingCredentials", err)
	}

	request.Header.Add("Authorization", "Bearer first")
	request.Header.Add("Authorization", "Bearer second")
	if _, err := ParseBearerToken(request); !errors.Is(err, ErrDuplicateCredentials) {
		t.Fatalf("duplicate header error = %v, want ErrDuplicateCredentials", err)
	}

	request.Header = http.Header{
		"Authorization": []string{"Bearer first"},
		"authorization": []string{"Bearer second"},
	}
	if _, err := ParseBearerToken(request); !errors.Is(err, ErrDuplicateCredentials) {
		t.Fatalf("case-variant duplicate error = %v, want ErrDuplicateCredentials", err)
	}

	request.Header = make(http.Header)
	request.Header.Set("Authorization", "Bearer "+testValue)
	got, err := ParseBearerToken(request)
	if err != nil {
		t.Fatalf("valid request: %v", err)
	}
	if got != testValue {
		t.Errorf("token = %q, want configured test token", got)
	}
}

func TestBearerSHA256AuthenticatesMatchingToken(t *testing.T) {
	t.Parallel()

	verifier := newTestVerifier(t, testValue, "service-a")
	request := requestWithBearer(testValue)

	principal, err := verifier.Authenticate(request.Context(), request)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal.Subject != "service-a" || len(principal.Scopes) != 0 {
		t.Errorf("principal = %#v, want service-a without scopes", principal)
	}
}

func TestBearerSHA256UsesDefaultSubject(t *testing.T) {
	t.Parallel()

	digest := tokenDigestHex(testValue)
	verifier, err := NewBearerSHA256(digest)
	if err != nil {
		t.Fatalf("NewBearerSHA256: %v", err)
	}
	request := requestWithBearer(testValue)
	principal, err := verifier.Authenticate(request.Context(), request)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal.Subject != DefaultBearerSubject {
		t.Errorf("subject = %q, want %q", principal.Subject, DefaultBearerSubject)
	}
}

func TestBearerSHA256RejectsMismatchesWithoutLeakingCredentials(t *testing.T) {
	t.Parallel()

	verifier := newTestVerifier(t, testValue, "service-a")
	candidates := []string{
		"X" + testValue[1:],
		testValue[:len(testValue)-1] + "X",
		testValue + "X",
		"different-token-with-a-valid-shape",
	}
	for index, candidate := range candidates {
		request := requestWithBearer(candidate)
		principal, err := verifier.Authenticate(request.Context(), request)
		if !isZeroPrincipal(principal) {
			t.Errorf("case %d principal = %#v, want zero", index, principal)
		}
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("case %d error = %v, want ErrInvalidCredentials", index, err)
		}
		if err != nil && strings.Contains(err.Error(), candidate) {
			t.Errorf("case %d error leaks supplied credential", index)
		}
	}
	if representation := fmt.Sprintf("%#v", verifier); strings.Contains(representation, testValue) {
		t.Error("verifier retains or formats the raw bearer token")
	}
}

func TestConstantTimeDigestComparisonChecksFixedSizeDigests(t *testing.T) {
	t.Parallel()

	expected := sha256.Sum256([]byte(testValue))
	if !constantTimeDigestEqual(expected, expected) {
		t.Fatal("equal digests did not match")
	}
	for index := range expected {
		mismatch := expected
		mismatch[index] ^= 0xff
		if constantTimeDigestEqual(expected, mismatch) {
			t.Fatalf("digest mismatch at byte %d matched", index)
		}
	}
}

func TestBearerSHA256RejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	validDigest := tokenDigestHex(testValue)
	tests := []struct {
		name    string
		digest  string
		subject string
		want    error
	}{
		{name: "empty digest", digest: "", subject: "service", want: ErrInvalidDigest},
		{name: "short digest", digest: strings.Repeat("a", sha256.Size*2-1), subject: "service", want: ErrInvalidDigest},
		{name: "long digest", digest: strings.Repeat("a", sha256.Size*2+1), subject: "service", want: ErrInvalidDigest},
		{name: "non hexadecimal digest", digest: strings.Repeat("z", sha256.Size*2), subject: "service", want: ErrInvalidDigest},
		{name: "leading digest whitespace", digest: " " + validDigest, subject: "service", want: ErrInvalidDigest},
		{name: "trailing digest whitespace", digest: validDigest + " ", subject: "service", want: ErrInvalidDigest},
		{name: "empty subject", digest: validDigest, subject: "", want: ErrInvalidPrincipal},
		{name: "subject edge whitespace", digest: validDigest, subject: " service", want: ErrInvalidPrincipal},
		{name: "subject control", digest: validDigest, subject: "service\nadmin", want: ErrInvalidPrincipal},
		{name: "subject invalid UTF-8", digest: validDigest, subject: string([]byte{0xff}), want: ErrInvalidPrincipal},
		{name: "oversized subject", digest: validDigest, subject: strings.Repeat("a", MaxPrincipalSubjectBytes+1), want: ErrInvalidPrincipal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			verifier, err := NewBearerSHA256WithSubject(test.digest, test.subject)
			if verifier != nil {
				t.Error("invalid configuration returned a verifier")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}

	uppercase, err := NewBearerSHA256WithSubject(strings.ToUpper(validDigest), "service")
	if err != nil || uppercase == nil {
		t.Fatalf("uppercase hexadecimal digest rejected: %v", err)
	}
}

func TestBearerSHA256HandlesNilInputsAndContextCancellation(t *testing.T) {
	t.Parallel()

	verifier := newTestVerifier(t, testValue, "service")
	validRequest := requestWithBearer(testValue)

	if _, err := verifier.Authenticate(nilAuthContext(), validRequest); !errors.Is(err, ErrNilContext) {
		t.Errorf("nil context error = %v, want ErrNilContext", err)
	}
	if _, err := verifier.Authenticate(context.Background(), nil); !errors.Is(err, ErrNilRequest) {
		t.Errorf("nil request error = %v, want ErrNilRequest", err)
	}
	var nilVerifier *BearerSHA256
	if _, err := nilVerifier.Authenticate(context.Background(), validRequest); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("nil verifier error = %v, want ErrInvalidCredentials", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifier.Authenticate(canceled, validRequest); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled context error = %v, want context.Canceled", err)
	}

	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()
	if _, err := verifier.Authenticate(expired, validRequest); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired context error = %v, want context.DeadlineExceeded", err)
	}

	requestContext, stopRequest := context.WithCancel(context.Background())
	request := requestWithBearer(testValue).WithContext(requestContext)
	stopRequest()
	if _, err := verifier.Authenticate(request.Context(), request); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled request context error = %v, want context.Canceled", err)
	}
}

func TestBearerSHA256IsSafeForConcurrentAuthentication(t *testing.T) {
	t.Parallel()

	verifier := newTestVerifier(t, testValue, "service")
	const workers = 32
	const iterations = 100
	var wait sync.WaitGroup
	errorsChannel := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				request := requestWithBearer(testValue)
				principal, err := verifier.Authenticate(request.Context(), request)
				if err != nil {
					errorsChannel <- err
					return
				}
				if principal.Subject != "service" {
					errorsChannel <- fmt.Errorf("unexpected subject %q", principal.Subject)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("concurrent Authenticate: %v", err)
	}
}

func TestPrincipalContextHelpersValidateAndDefensivelyCopy(t *testing.T) {
	t.Parallel()

	original := Principal{Subject: "user-123", Scopes: []string{"items:read", "items:write"}}
	ctx, err := ContextWithPrincipal(context.Background(), original)
	if err != nil {
		t.Fatalf("ContextWithPrincipal: %v", err)
	}
	original.Scopes[0] = "admin"

	stored, ok := PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("principal not found in context")
	}
	if stored.Subject != "user-123" || !stored.HasScope("items:read") || stored.HasScope("admin") {
		t.Fatalf("stored principal = %#v, want unchanged defensive copy", stored)
	}
	if stored.HasScope("") || stored.HasScope("ITEMS:READ") {
		t.Error("HasScope must use non-empty exact matches")
	}

	stored.Scopes[0] = "mutated"
	again, ok := PrincipalFromContext(ctx)
	if !ok || !again.HasScope("items:read") || again.HasScope("mutated") {
		t.Fatalf("context principal was mutated through retrieved slice: %#v", again)
	}

	aliasContext := WithPrincipal(context.Background(), Principal{Subject: "alias"})
	if alias, ok := PrincipalFromContext(aliasContext); !ok || alias.Subject != "alias" {
		t.Fatalf("WithPrincipal alias stored %#v, ok=%t", alias, ok)
	}
}

func TestPrincipalValidationBoundaries(t *testing.T) {
	t.Parallel()

	valid := []Principal{
		{Subject: strings.Repeat("s", MaxPrincipalSubjectBytes)},
		{Subject: "unicode-主体"},
		{Subject: "scoped", Scopes: []string{strings.Repeat("x", MaxPrincipalScopeBytes)}},
		{Subject: "many-scopes", Scopes: repeatedScopes(MaxPrincipalScopes, "x")},
		{Subject: "total-scopes", Scopes: repeatedScopes(MaxPrincipalScopeTotalBytes/MaxPrincipalScopeBytes, strings.Repeat("x", MaxPrincipalScopeBytes))},
	}
	for index, principal := range valid {
		if err := principal.Validate(); err != nil {
			t.Errorf("valid principal %d: %v", index, err)
		}
	}

	invalid := []Principal{
		{},
		{Subject: " "},
		{Subject: " subject"},
		{Subject: "subject\x00suffix"},
		{Subject: string([]byte{0xff})},
		{Subject: strings.Repeat("s", MaxPrincipalSubjectBytes+1)},
		{Subject: "scoped", Scopes: []string{""}},
		{Subject: "scoped", Scopes: []string{" scope"}},
		{Subject: "scoped", Scopes: []string{"scope\nadmin"}},
		{Subject: "scoped", Scopes: []string{strings.Repeat("x", MaxPrincipalScopeBytes+1)}},
		{Subject: "many-scopes", Scopes: repeatedScopes(MaxPrincipalScopes+1, "x")},
		{Subject: "total-scopes", Scopes: repeatedScopes(MaxPrincipalScopeTotalBytes/MaxPrincipalScopeBytes+1, strings.Repeat("x", MaxPrincipalScopeBytes))},
	}
	for index, principal := range invalid {
		if err := principal.Validate(); !errors.Is(err, ErrInvalidPrincipal) {
			t.Errorf("invalid principal %d error = %v, want ErrInvalidPrincipal", index, err)
		}
	}
}

func TestPrincipalContextHelpersHandleMissingAndInvalidInputs(t *testing.T) {
	t.Parallel()

	if ctx, err := ContextWithPrincipal(nilAuthContext(), Principal{Subject: "user"}); ctx != nil || !errors.Is(err, ErrNilContext) {
		t.Fatalf("nil context result = %#v, %v; want nil, ErrNilContext", ctx, err)
	}
	if ctx, err := ContextWithPrincipal(context.Background(), Principal{}); ctx != nil || !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("invalid principal result = %#v, %v; want nil, ErrInvalidPrincipal", ctx, err)
	}
	if principal, ok := PrincipalFromContext(nilAuthContext()); ok || !isZeroPrincipal(principal) {
		t.Fatalf("nil context principal = %#v, ok=%t", principal, ok)
	}
	if principal, ok := PrincipalFromContext(context.Background()); ok || !isZeroPrincipal(principal) {
		t.Fatalf("empty context principal = %#v, ok=%t", principal, ok)
	}
}

func newTestVerifier(t *testing.T, token, subject string) *BearerSHA256 {
	t.Helper()
	verifier, err := NewBearerSHA256WithSubject(tokenDigestHex(token), subject)
	if err != nil {
		t.Fatalf("construct verifier: %v", err)
	}
	return verifier
}

func tokenDigestHex(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func requestWithBearer(token string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func repeatedScopes(count int, value string) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func isZeroPrincipal(principal Principal) bool {
	return principal.Subject == "" && len(principal.Scopes) == 0
}

func nilAuthContext() context.Context { return nil }
