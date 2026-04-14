# Copilot Instructions

## Build, Test, and Lint

```bash
# Build (cross-compiled for Linux/amd64 Lambda)
make build

# Run all tests
make test
go test ./...

# Run a single test
go test ./lrtr/ -run TestRouter
go test ./lreq/ -run Test_UnmarshalReq
go test ./lmw/ljwt/ -run TestSign
go test ./lres/ -run TestErrorRes

# Format code
make format   # runs: gofmt -s -w -l .

# Tidy dependencies
make tidy

# Full pipeline
make all      # format → tidy → build → test
```

**⚠️ Tests requiring external services:** Tests in `lmw/`, `lmw/ljwt/`, and `internal/examples/books/` load `.env` at startup using `godotenv`. The books tests also connect to MongoDB (`CONNECTION_STRING`). Without a `.env` file and reachable MongoDB, those packages will fail. Tests in `lrtr/`, `lreq/`, and `lres/` have no external dependencies.

## Architecture

This is a Go library (not an application) that provides an AWS Lambda routing framework with JWT authentication. The module path is `github.com/seantcanavan/lambda_jwt_router`.

**Package layout:**

| Package | Purpose |
|---|---|
| `lrtr` | Core router — `NewRouter`, `Route`, `Handler` (Lambda entry point), `ServeHTTP` (local dev) |
| `lmw` | Middleware — `InjectLambdaContextMW`, `LogRequestMW`, `DecodeStandardMW`, `DecodeExpandedMW`, `AllowOptionsMW` |
| `lmw/ljwt` | JWT primitives — `Sign`, `VerifyJWT`, `ExtractJWT`, `ExtractStandard`, `ExtractCustom`, `ExtendStandard`, `ExtendExpanded`, `ExpandedClaims` |
| `lreq` | Request unmarshalling — `UnmarshalReq`, `MarshalReq` |
| `lres` | Response helpers — `Success`, `Error`, `Custom`, `StatusAndError`, `Empty`, `File`, `FileB64`, `Unmarshal` |
| `lcom` | Shared constants, types, and errors — `Handler`, `Middleware`, all context key constants, all env var name constants, all sentinel errors |
| `internal/util` | Test helpers — random struct/claims generators, `WrapErrors`, shared mock struct types |
| `internal/examples` | Full end-to-end usage examples (routing, middleware, JWT, database/MongoDB) |

**Request flow:**
`lambda.Start(router.Handler)` → `Router.Handler` → route matched by regex → middleware chain assembled in reverse (global outermost, per-route innermost) → handler executed.

The same `Router` implements `net/http.Handler` via `ServeHTTP` for local development. The pattern in all examples: check `STAGE` env var and call `lambda.Start(router.Handler)` for staging/production, or `http.ListenAndServe(..., router.ServeHTTP)` otherwise.

**Layer separation pattern** (shown in `internal/examples/books`):
- `books.go` — pure business logic, no Lambda types
- `books_lambda.go` — thin Lambda adapter: `UnmarshalReq` → call business logic → `lres.*`
- `books_test.go` — unit tests for business logic
- `books_lambda_test.go` — integration tests via the Lambda adapter functions

## Key Conventions

### `lambda` struct tags for request unmarshalling
Use `lambda:"<source>.<key>"` tags to bind path, query, and header values to struct fields. Combine with `json` tags for body fields on the same struct:
```go
type UpdateBookInput struct {
    ID    primitive.ObjectID `lambda:"path.id"`               // path param → ObjectID
    Page  int64              `lambda:"query.page"`            // query string → int64
    Size  *int64             `lambda:"query.page_size"`       // optional pointer
    Lang  string             `lambda:"header.Accept-Language"` // header → string
    Tags  []string           `lambda:"query.tags"`            // multi-value query → slice
    Title string             `json:"title"`                   // JSON body field
}
// UnmarshalReq(req, true, &input)  — parse JSON body AND lambda tags
// UnmarshalReq(req, false, &input) — lambda tags only, no body
```

Valid tag sources: `path`, `query`, `header`.

Supported field types and their special behaviors:
- All scalar types: `string`, `bool`, all `int*`, `uint*`, `float*` variants, and custom types aliasing them
- Pointer variants of all above: nil if the key is absent from the request; populated if present
- `time.Time` / `*time.Time`: parsed from RFC3339 format (`2006-01-02T15:04:05Z`)
- `civil.Date` / `*civil.Date`: parsed from `YYYY-MM-DD` format
- `primitive.ObjectID` / `*primitive.ObjectID`: parsed via `primitive.ObjectIDFromHex`
- Slices: populated from `MultiValueQueryStringParameters` when using `query` source, or by splitting a single comma-separated value (e.g., `"one,two,three"` → `[]string{"one", "two", "three"}`)
- Pointer-to-string custom types (e.g., `[]*Number`): split from comma-separated single value
- Booleans accept (case-insensitive): `"1"`, `"true"`, `"on"`, `"enabled"`, `"t"` → `true`; anything else → `false`

Empty string values for pointer types (`*time.Time`, `*civil.Date`, `*primitive.ObjectID`) result in `nil`, not an error.

### Response helpers — always use `lres`, never construct responses manually
```go
return lres.Success(data)                              // 200 + JSON body
return lres.Error(err)                                 // HTTPError.Status or 500
return lres.StatusAndError(http.StatusBadRequest, err) // explicit status + error body
return lres.Custom(http.StatusCreated, headers, data)  // full control
return lres.Empty()                                    // 200 + empty JSON `{}`
return lres.File("text/csv", headers, fileBytes)       // raw bytes, IsBase64Encoded=false
return lres.FileB64("image/png", headers, imgBytes)    // base64 encoded, IsBase64Encoded=true
```

Error response body format is always `{"status": 400, "message": "..."}`.

`lres.ExposeServerErrors` (global `bool`, default `true`) — when `false`, responses with status ≥ 500 return the HTTP status text instead of the actual error message. Use `lres.HTTPError` as the error type when you want to control the status code of an error response.

### Route matching behavior
Routes are stored in a `map[string]route` keyed by the path pattern string. Path parameters use `:<name>` syntax and compile to `([^/]+)` regex groups:
- `/books/:id` → `^/api/books/([^/]+)$`
- `/:id/stuff/:fake` → `^/api/([^/]+)/stuff/([^/]+)$`

**Trailing slashes** on incoming requests are stripped before matching.

**Method vs path mismatch errors:** If the path matches but the method is not registered, returns 405 (not 404). If the path itself doesn't match any route, returns 404.

**Overlapping routes:** When a literal route (e.g., `/foo/bar`) and a parameterized route (e.g., `/foo/:id`) both match the same URL, the router iterates a map (non-deterministic order). The correct route wins because method matching selects the route that has both a matching path AND matching HTTP method registered. The 405/404 `negErr` is only returned if no route fully matches.

### Middleware chaining
```go
type Middleware func(Handler) Handler

// Global middleware is applied outermost; per-route middleware is innermost.
// Execution order (wrapping applied in reverse index order):
//   global[0] → global[1] → per-route[0] → handler
```

Middleware receives `next lcom.Handler` and returns a `lcom.Handler`. Call `next(ctx, req)` to continue the chain. Returning early (without calling `next`) short-circuits the chain — this is how `AllowOptionsMW` works (returns 200 immediately, bypassing auth middleware on OPTIONS requests).

**Typical global middleware setup:**
```go
router = lrtr.NewRouter("/api", lmw.InjectLambdaContextMW)
router.Route("GET", "/books/:id", books.GetLambda, lmw.DecodeStandardMW)
```
`InjectLambdaContextMW` runs first (outermost), populates context, then `DecodeStandardMW` validates JWT, then the handler runs.

**Recommended logging setup:** Wrap handlers with `InjectLambdaContextMW` (global) + `LogRequestMW` (per-route or global). `LogRequestMW` must run after `InjectLambdaContextMW` so it can read the context values it logs.

### JWT flow
All JWT operations use HMAC-SHA512. The secret (`LAMBDA_JWT_ROUTER_HMAC_SECRET`) must be hex-encoded — `ljwt` calls `hex.DecodeString` on it. If missing or invalid hex, the app calls `log.Fatalf`.

```
// Create a JWT
claims := ljwt.ExtendExpanded(myExpandedClaims)   // or ljwt.ExtendStandard(...)
claims["customField"] = "value"                    // add arbitrary fields
signedJWT, err := ljwt.Sign(claims)

// Decode via middleware (sets values in ctx)
lmw.DecodeStandardMW  → injects jwt.StandardClaims fields into ctx
lmw.DecodeExpandedMW  → injects ljwt.ExpandedClaims fields into ctx (adds email, firstName, fullName, level, userType)

// Read claims from context
email := ctx.Value(lcom.JWTClaimEmailKey).(string)

// Manual extraction (e.g., for custom claims)
mapClaims, httpStatus, err := ljwt.ExtractJWT(req.Headers)
var myClaims MyCustomClaims
err = ljwt.ExtractCustom(mapClaims, &myClaims)
```

**Authorization header format is strict:** Must be exactly `"Authorization"` (mixed case), with value `"Bearer <token>"` — capital B, exactly one space between `Bearer` and the token. Lowercase (`authorization`, `bearer`) or missing space causes 400 errors.

### CORS
Every `router.Route(...)` call auto-registers an `OPTIONS` handler for that path (using `AllowOptionsMW`) unless `LAMBDA_JWT_ROUTER_NO_CORS=true`. The OPTIONS handler always returns 200 and bypasses all other middleware, ensuring preflight requests succeed even on auth-protected routes.

CORS headers are injected into **every** `lres.*` response (via `lres.addCors`) when the corresponding env vars are set.

### Context keys
All context keys are string constants in `lcom`:
- Lambda request info (set by `InjectLambdaContextMW`): `LambdaContextIDKey`, `LambdaContextMethodKey`, `LambdaContextPathKey`, `LambdaContextPathParamsKey`, `LambdaContextQueryParamsKey`, `LambdaContextMultiParamsKey`, `LambdaContextRequestIDKey`, `LambdaContextUserIDKey`, `LambdaContextUserTypeKey`
- JWT claims (set by `DecodeStandardMW`/`DecodeExpandedMW`): `JWTClaimAudienceKey`, `JWTClaimExpiresAtKey`, `JWTClaimIDKey`, `JWTClaimIssuedAtKey`, `JWTClaimIssuerKey`, `JWTClaimNotBeforeKey`, `JWTClaimSubjectKey`, and for expanded: `JWTClaimEmailKey`, `JWTClaimFirstNameKey`, `JWTClaimFullNameKey`, `JWTClaimLevelKey`, `JWTClaimUserTypeKey`

`InjectLambdaContextMW` uses `lcom.LambdaParams` internally — it extracts `id`, `userId`, `userType` from path, query, and body simultaneously, using `chooseLongest` to pick the longest non-empty value when the same field appears in multiple places.

Context values are only set if non-empty strings (non-nil for non-string types). Missing values are silently skipped — always type-assert defensively or check for nil before asserting.

### Error sentinel values
All error variables are in `lcom`. They all use `%w` so `errors.Is` traversal works:
```go
errors.Is(err, lcom.ErrNoAuthorizationHeader)
errors.Is(err, lcom.ErrNoBearerPrefix)
errors.Is(err, lcom.ErrInvalidJWT)
errors.Is(err, lcom.ErrBadClaimsObject)
// etc.
```
`internal/util.WrapErrors(err1, err2)` formats as `err1.Error() + ": %w"` wrapping `err2` — used throughout `ljwt` to chain errors.

### Testing patterns
- Tests that need env vars load `.env` in `TestMain` via `godotenv.Load("../.env")` (relative path depth varies by package).
- Use `internal/util` generators to build randomized test data: `GenerateRandomAPIGatewayProxyRequest()`, `GenerateRandomAPIGatewayContext()`, `GenerateExpandedMapClaims()`, `GenerateStandardMapClaims()`, `GenerateRandomString(n)`, `GenerateRandomInt(N, M)`.
- Use `lreq.MarshalReq(struct)` to construct a `APIGatewayProxyRequest` with a JSON body for testing POST/PUT handlers.
- Use `lres.Unmarshal(res, &target)` to decode response bodies in tests.
- To test middleware that sets context values: pass a dummy `lcom.Handler` that reads the expected context keys and marshals them into the response body, then use `lres.Unmarshal` to verify.
- All tests use `github.com/stretchr/testify/require`.

### Environment variables (see `.env.example`)
| Var | Purpose |
|---|---|
| `LAMBDA_JWT_ROUTER_HMAC_SECRET` | Hex-encoded binary HMAC secret for JWT sign/verify (required for JWT operations) |
| `LAMBDA_JWT_ROUTER_CORS_ORIGIN` | `Access-Control-Allow-Origin` response header value |
| `LAMBDA_JWT_ROUTER_CORS_METHODS` | `Access-Control-Allow-Methods` response header value |
| `LAMBDA_JWT_ROUTER_CORS_HEADERS` | `Access-Control-Allow-Headers` response header value |
| `LAMBDA_JWT_ROUTER_NO_CORS` | Set to `"true"` to disable auto-OPTIONS handlers and CORS header injection |
| `STAGE` | `"staging"` or `"production"` → `lambda.Start`; anything else → HTTP server on `PORT` (default 8080) |
| `CONNECTION_STRING` | MongoDB connection string (required only for `internal/examples/database` and books tests) |
