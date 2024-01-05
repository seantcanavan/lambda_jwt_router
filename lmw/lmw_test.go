package lmw

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/golang-jwt/jwt"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/seantcanavan/lambda_jwt_router/internal/util"
	"github.com/seantcanavan/lambda_jwt_router/lcom"
	"github.com/seantcanavan/lambda_jwt_router/lmw/ljwt"
	"github.com/seantcanavan/lambda_jwt_router/lres"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	setup()
	m.Run()
}

func setup() {
	err := godotenv.Load("../.env")
	if err != nil {
		log.Fatal().Msg(fmt.Sprintf("Unable to load .env file: %s", err))
	}
}

func TestAllowOptionsMW(t *testing.T) {
	t.Run("verify empty OPTIONS req succeeds", func(t *testing.T) {
		req := events.APIGatewayProxyRequest{
			HTTPMethod:     http.MethodOptions,
			Headers:        nil,
			RequestContext: util.GenerateRandomAPIGatewayContext(),
		}

		// we pass along an error handler but expect http.StatusOK because the AllowOptions handler should execute first
		jwtMiddlewareHandler := AllowOptionsMW()
		res, err := jwtMiddlewareHandler(nil, req)
		require.Nil(t, err)
		require.Equal(t, res.StatusCode, http.StatusOK)
	})
	t.Run("verify OPTIONS req succeeds with invalid JWT for AllowOptions", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		req := events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodOptions,
			Headers: map[string]string{
				"Authorization": "Bearer " + "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNzA0NDE0MjQwLCJleHAiOjE3MDQ1MDA2NDB9.uzTEDZqlRMZk1d6qvH4LXSGuV2ujHEQPckD0ahaQCWXBvyCjEuXDX8IKUt23KFVz6SZiKckp1AtIrscrE9NVsw",
			},
			RequestContext: util.GenerateRandomAPIGatewayContext(),
		}

		// we pass along an error handler but expect http.StatusOK because the AllowOptions handler should execute first
		jwtMiddlewareHandler := AllowOptionsMW()
		res, err := jwtMiddlewareHandler(ctx, req)
		require.Nil(t, err)
		require.Equal(t, res.StatusCode, http.StatusOK)
	})
	t.Run("verify OPTIONS req succeeds with no Authorization header for AllowOptions", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		req := events.APIGatewayProxyRequest{
			HTTPMethod:     http.MethodOptions,
			Headers:        nil,
			RequestContext: util.GenerateRandomAPIGatewayContext(),
		}

		// we pass along an error handler but expect http.StatusOK because the AllowOptions handler should execute first
		jwtMiddlewareHandler := AllowOptionsMW()
		res, err := jwtMiddlewareHandler(ctx, req)
		require.Nil(t, err)
		require.Equal(t, res.StatusCode, http.StatusOK)
	})
}

func TestDecodeAndInjectExpandedClaims(t *testing.T) {
	t.Run("verify error is returned by DecodeExpandedMW when missing Authorization header", func(t *testing.T) {
		req := events.APIGatewayProxyRequest{}
		jwtMiddlewareHandler := DecodeExpandedMW(generateEmptyErrorHandler())
		res, err := jwtMiddlewareHandler(nil, req)
		require.Nil(t, err)
		require.Equal(t, res.StatusCode, http.StatusBadRequest)

		var responseBody lres.HTTPError
		err = lres.UnmarshalRes(res, &responseBody)
		require.Nil(t, err)

		require.Equal(t, responseBody.Status, res.StatusCode)
		require.Equal(t, responseBody.Message, lcom.ErrNoAuthorizationHeader.Error())
	})
	t.Run("verify context is returned by DecodeExpandedMW with a signed JWT", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		expandedClaims := util.GenerateExpandedMapClaims()

		signedJWT, err := ljwt.Sign(expandedClaims)
		require.Nil(t, err)

		req := events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodGet,
			Headers: map[string]string{
				"Authorization": "Bearer " + signedJWT,
			},
			RequestContext: util.GenerateRandomAPIGatewayContext(),
		}

		jwtMiddlewareHandler := DecodeExpandedMW(GenerateSuccessHandlerAndMapExpandedContext())
		res, err := jwtMiddlewareHandler(ctx, req)
		require.Nil(t, err)
		require.Equal(t, res.StatusCode, http.StatusOK)

		var returnedClaims ljwt.ExpandedClaims
		err = lres.UnmarshalRes(res, &returnedClaims)
		require.Nil(t, err)
		// this verifies that the context gets set in the middleware inject function since the
		// dummy handler passed to it as the 'next' call injects the values from its passed
		// context object into the response body. The function doesn't work this way in practice
		// however it does allow me to fully unit test it to make sure the context setting is working.
		// It's hacky and I'm not proud of it but I'm not sure how else to do it.
		require.Equal(t, expandedClaims[lcom.JWTClaimAudienceKey], returnedClaims.Audience)
		require.Equal(t, expandedClaims[lcom.JWTClaimEmailKey], returnedClaims.Email)
		require.Equal(t, expandedClaims[lcom.JWTClaimExpiresAtKey], returnedClaims.ExpiresAt)
		require.Equal(t, expandedClaims[lcom.JWTClaimFirstNameKey], returnedClaims.FirstName)
		require.Equal(t, expandedClaims[lcom.JWTClaimFullNameKey], returnedClaims.FullName)
		require.Equal(t, expandedClaims[lcom.JWTClaimIDKey], returnedClaims.ID)
		require.Equal(t, expandedClaims[lcom.JWTClaimIssuedAtKey], returnedClaims.IssuedAt)
		require.Equal(t, expandedClaims[lcom.JWTClaimIssuerKey], returnedClaims.Issuer)
		require.Equal(t, expandedClaims[lcom.JWTClaimLevelKey], returnedClaims.Level)
		require.Equal(t, expandedClaims[lcom.JWTClaimNotBeforeKey], returnedClaims.NotBefore)
		require.Equal(t, expandedClaims[lcom.JWTClaimSubjectKey], returnedClaims.Subject)
		require.Equal(t, expandedClaims[lcom.JWTClaimUserTypeKey], returnedClaims.UserType)
	})
}

// generateSuccessHandlerAndMapExpandedContext returns a middleware handler
// that takes the values inserted into the context object by DecodeExpandedMW
// and returns them as an object from the request so that unit tests can analyze the values
// and make sure they have done the full trip from JWT -> CTX -> unit test
func GenerateSuccessHandlerAndMapExpandedContext() lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		events.APIGatewayProxyResponse,
		error) {
		return lres.CustomRes(http.StatusOK, nil, ljwt.ExpandedClaims{
			Audience:  ctx.Value(lcom.JWTClaimAudienceKey).(string),
			Email:     ctx.Value(lcom.JWTClaimEmailKey).(string),
			ExpiresAt: ctx.Value(lcom.JWTClaimExpiresAtKey).(int64),
			FirstName: ctx.Value(lcom.JWTClaimFirstNameKey).(string),
			FullName:  ctx.Value(lcom.JWTClaimFullNameKey).(string),
			ID:        ctx.Value(lcom.JWTClaimIDKey).(string),
			IssuedAt:  ctx.Value(lcom.JWTClaimIssuedAtKey).(int64),
			Issuer:    ctx.Value(lcom.JWTClaimIssuerKey).(string),
			Level:     ctx.Value(lcom.JWTClaimLevelKey).(string),
			NotBefore: ctx.Value(lcom.JWTClaimNotBeforeKey).(int64),
			Subject:   ctx.Value(lcom.JWTClaimSubjectKey).(string),
			UserType:  ctx.Value(lcom.JWTClaimUserTypeKey).(string),
		})
	}
}

func TestDecodeAndInjectStandardClaims(t *testing.T) {
	t.Run("verify error is returned by DecodeStandardMW when missing Authorization header", func(t *testing.T) {
		req := events.APIGatewayProxyRequest{}
		jwtMiddlewareHandler := DecodeStandardMW(generateEmptyErrorHandler())
		res, err := jwtMiddlewareHandler(nil, req)
		require.Nil(t, err)
		require.Equal(t, res.StatusCode, http.StatusBadRequest)

		var responseBody lres.HTTPError
		err = lres.UnmarshalRes(res, &responseBody)
		require.Nil(t, err)

		require.Equal(t, responseBody.Status, res.StatusCode)
		require.Equal(t, responseBody.Message, lcom.ErrNoAuthorizationHeader.Error())
	})
	t.Run("verify context is returned by DecodeStandardMW with a signed JWT", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		standardClaims := util.GenerateStandardMapClaims()

		signedJWT, err := ljwt.Sign(standardClaims)
		require.Nil(t, err)

		req := events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodGet,
			Headers: map[string]string{
				"Authorization": "Bearer " + signedJWT,
			},
			RequestContext: util.GenerateRandomAPIGatewayContext(),
		}

		jwtMiddlewareHandler := DecodeStandardMW(generateSuccessHandlerAndMapStandardContext(t))
		res, err := jwtMiddlewareHandler(ctx, req)
		require.Nil(t, err)
		require.Equal(t, res.StatusCode, http.StatusOK)

		var returnedClaims jwt.StandardClaims
		err = lres.UnmarshalRes(res, &returnedClaims)
		require.Nil(t, err)
		// this verifies that the context gets set in the middleware inject function since the
		// dummy handler passed to it as the 'next' call injects the values from its passed
		// context object into the response body. The function doesn't work this way in practice
		// however it does allow me to fully unit test it to make sure the context setting is working.
		// It's hacky and I'm not proud of it but I'm not sure how else to do it.
		require.Equal(t, returnedClaims.Audience, standardClaims[lcom.JWTClaimAudienceKey])
		require.Equal(t, returnedClaims.ExpiresAt, standardClaims[lcom.JWTClaimExpiresAtKey])
		require.Equal(t, returnedClaims.Id, standardClaims[lcom.JWTClaimIDKey])
		require.Equal(t, returnedClaims.IssuedAt, standardClaims[lcom.JWTClaimIssuedAtKey])
		require.Equal(t, returnedClaims.Issuer, standardClaims[lcom.JWTClaimIssuerKey])
		require.Equal(t, returnedClaims.NotBefore, standardClaims[lcom.JWTClaimNotBeforeKey])
		require.Equal(t, returnedClaims.Subject, standardClaims[lcom.JWTClaimSubjectKey])
	})
}

func TestGenerateEmptyErrorHandler(t *testing.T) {
	t.Run("verify empty error handler returns error", func(t *testing.T) {
		errHandler := generateEmptyErrorHandler()
		res, err := errHandler(nil, util.GenerateRandomAPIGatewayProxyRequest())
		require.Nil(t, err) // err handler embeds the error in the response, not the golang stack
		require.Equal(t, res.StatusCode, http.StatusInternalServerError)
		require.Nil(t, err)
	})
}

func TestGenerateEmptySuccessHandler(t *testing.T) {
	t.Run("verify empty success handler returns success", func(t *testing.T) {
		successHandler := generateEmptySuccessHandler()
		res, err := successHandler(nil, util.GenerateRandomAPIGatewayProxyRequest())
		require.Nil(t, err)
		require.Equal(t, res.StatusCode, http.StatusOK)
		require.Equal(t, res.Body, "") // empty struct response
	})
}

// generateEmptyErrorHandler generates a lcom.Handler function that returns a 500 error response
func generateEmptyErrorHandler() lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		events.APIGatewayProxyResponse,
		error) {

		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
		}, nil
	}
}

// generateEmptySuccessHandler generates a lcom.Handler function that returns a 200 success response
func generateEmptySuccessHandler() lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		events.APIGatewayProxyResponse,
		error) {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusOK,
		}, nil
	}
}

// generateSuccessHandlerAndMapStandardContext returns a middleware handler
// that takes the values inserted into the context object by DecodeStandardMW
// and returns them as an object from the request so that unit tests can analyze the values
// and make sure they have done the full trip from JWT -> CTX -> unit test
func generateSuccessHandlerAndMapStandardContext(t *testing.T) lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		events.APIGatewayProxyResponse,
		error) {

		jsonBytes, err := json.Marshal(jwt.StandardClaims{
			Audience:  ctx.Value(lcom.JWTClaimAudienceKey).(string),
			ExpiresAt: ctx.Value(lcom.JWTClaimExpiresAtKey).(int64),
			Id:        ctx.Value(lcom.JWTClaimIDKey).(string),
			IssuedAt:  ctx.Value(lcom.JWTClaimIssuedAtKey).(int64),
			Issuer:    ctx.Value(lcom.JWTClaimIssuerKey).(string),
			NotBefore: ctx.Value(lcom.JWTClaimNotBeforeKey).(int64),
			Subject:   ctx.Value(lcom.JWTClaimSubjectKey).(string),
		})
		require.NoError(t, err)

		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusOK,
			Body:       string(jsonBytes),
		}, nil
	}
}

func TestLogRequestMW(t *testing.T) {
	// Setup Zerolog to write to a file
	logFile := "test_log.txt"
	file, _ := os.Create(logFile)

	defer func() {
		err := file.Close()
		if err != nil {
			panic(err)
		}
	}()

	defer func() {
		err := os.Remove(logFile)
		if err != nil {
			panic(err)
		}
	}()

	log.Logger = zerolog.New(file)

	testCases := []struct {
		name           string
		expectedErr    error
		expectedStatus int
		expectedString string
		mockResponse   events.APIGatewayProxyResponse
		request        events.APIGatewayProxyRequest
	}{
		{
			name:           "Successful Request",
			expectedErr:    nil,
			expectedStatus: 200,
			expectedString: "200",
			mockResponse:   events.APIGatewayProxyResponse{StatusCode: 200},
			request:        util.GenerateRandomAPIGatewayProxyRequest(),
		},
		{
			name:           "400 - Path",
			expectedErr:    nil,
			expectedStatus: 400,
			expectedString: "/hi/there",
			mockResponse:   events.APIGatewayProxyResponse{StatusCode: 400},
			request: func() events.APIGatewayProxyRequest {
				a := util.GenerateRandomAPIGatewayProxyRequest()
				a.Path = "/hi/there"
				return a
			}(),
		},
		{
			name:           "400 - Path Parameters",
			expectedErr:    nil,
			expectedStatus: 400,
			expectedString: "\"pathParams\":{\"pathKey\":\"pathVal\"}",
			mockResponse:   events.APIGatewayProxyResponse{StatusCode: 400},
			request: func() events.APIGatewayProxyRequest {
				a := util.GenerateRandomAPIGatewayProxyRequest()
				a.PathParameters = map[string]string{
					"pathKey": "pathVal",
				}
				return a
			}(),
		},
		{
			name:           "400 - Query String Parameters",
			expectedErr:    nil,
			expectedStatus: 400,
			expectedString: "\"queryParams\":{\"qspKey\":\"qspVal\"}",
			mockResponse:   events.APIGatewayProxyResponse{StatusCode: 400},
			request: func() events.APIGatewayProxyRequest {
				a := util.GenerateRandomAPIGatewayProxyRequest()
				a.QueryStringParameters = map[string]string{
					"qspKey": "qspVal",
				}
				return a
			}(),
		},
		{
			name:           "400 - Multi Value Query String Parameters",
			expectedErr:    nil,
			expectedStatus: 400,
			expectedString: "\"multiParams\":{\"mvqspKey\":[\"mvqspVal1\",\"mvqspVal2\"]}",
			mockResponse:   events.APIGatewayProxyResponse{StatusCode: 400},
			request: func() events.APIGatewayProxyRequest {
				a := util.GenerateRandomAPIGatewayProxyRequest()
				a.MultiValueQueryStringParameters = map[string][]string{
					"mvqspKey": {"mvqspVal1", "mvqspVal2"},
				}
				return a
			}(),
		},
		{
			name:           "Server Error",
			expectedErr:    nil,
			expectedStatus: 500,
			expectedString: "500",
			mockResponse:   events.APIGatewayProxyResponse{StatusCode: 500},
			request:        util.GenerateRandomAPIGatewayProxyRequest(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup middleware with the mock handler
			middleware := InjectLambdaContextMW(LogRequestMW(mockHandler(tc.mockResponse, tc.expectedErr)))

			// Invoke middleware with test request
			_, _ = middleware(context.Background(), tc.request)

			// Read log file
			logContents, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatalf("Failed to read log file: %v", err)
			}

			strContents := string(logContents)

			require.Contains(t, strContents, tc.request.HTTPMethod)
			require.Contains(t, strContents, tc.expectedString)
			require.Contains(t, strContents, tc.request.RequestContext.RequestID)
			require.Contains(t, strContents, tc.request.Path)
			require.Contains(t, strContents, strconv.Itoa(tc.expectedStatus))
		})
	}
}

func mockHandler(response events.APIGatewayProxyResponse, err error) lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		return response, err
	}
}
