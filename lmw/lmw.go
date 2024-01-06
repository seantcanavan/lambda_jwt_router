package lmw

import (
	"context"
	"encoding/json"
	"github.com/aws/aws-lambda-go/events"
	"github.com/golang-jwt/jwt"
	"github.com/rs/zerolog/log"
	"github.com/seantcanavan/lambda_jwt_router/lcom"
	"github.com/seantcanavan/lambda_jwt_router/lmw/ljwt"
	"github.com/seantcanavan/lambda_jwt_router/lreq"
	"github.com/seantcanavan/lambda_jwt_router/lres"
	"net/http"
)

func LogRequestMW(next lcom.Handler) lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		res events.APIGatewayProxyResponse,
		err error,
	) {
		res, err = next(ctx, req)
		if err != nil {
			res.StatusCode = http.StatusInternalServerError
		}

		ctxVals := []string{
			lcom.LambdaContextIDKey,
			lcom.LambdaContextMethodKey,
			lcom.LambdaContextMultiParamsKey,
			lcom.LambdaContextPathKey,
			lcom.LambdaContextPathParamsKey,
			lcom.LambdaContextQueryParamsKey,
			lcom.LambdaContextRequestIDKey,
			lcom.LambdaContextUserIDKey,
			lcom.LambdaContextUserTypeKey,
		}

		logContextValues(ctx, res, ctxVals)

		return res, err
	}
}

func logContextValues(ctx context.Context, res events.APIGatewayProxyResponse, ctxVals []string) {
	event := log.Info()
	if res.StatusCode > 399 {
		event = log.Error()
		body, err := json.Marshal(res.Body)
		if err == nil {
			event.RawJSON("body", body)
		}
	}

	for _, currentKey := range ctxVals {
		val := ctx.Value(currentKey)
		if val != nil {
			event.Interface(currentKey, val)
		}
	}

	event.Int("statusCode", res.StatusCode)
	event.Msg("request finished")
}

// InjectLambdaContextMW with do exactly that - inject all appropriate lambda values into the local
// context so that other users down the line can query the context for things like HTTP method or Path
func InjectLambdaContextMW(next lcom.Handler) lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		res events.APIGatewayProxyResponse,
		err error,
	) {
		lambdaParams := &lcom.LambdaParams{}
		// we ignore the error in case the requests is empty or malformed - it's not up to us to crash the stack
		_ = lreq.UnmarshalReq(req, true, lambdaParams)

		vals := map[string]any{
			lcom.LambdaContextIDKey:          lambdaParams.GetID(),
			lcom.LambdaContextMethodKey:      req.HTTPMethod,
			lcom.LambdaContextMultiParamsKey: req.MultiValueQueryStringParameters,
			lcom.LambdaContextPathKey:        req.Path,
			lcom.LambdaContextPathParamsKey:  req.PathParameters,
			lcom.LambdaContextQueryParamsKey: req.QueryStringParameters,
			lcom.LambdaContextRequestIDKey:   req.RequestContext.RequestID,
			lcom.LambdaContextUserIDKey:      lambdaParams.GetUserID(),
			lcom.LambdaContextUserTypeKey:    lambdaParams.GetUserType(),
		}

		ctx = addToContext(ctx, vals)

		return next(ctx, req)
	}
}

func addToContext(ctx context.Context, vals map[string]any) context.Context {
	for key, val := range vals {
		if val == nil {
			continue
		}

		strVal, ok := val.(string)
		if ok {
			if strVal != "" {
				ctx = context.WithValue(ctx, key, val)
			}
		} else {
			ctx = context.WithValue(ctx, key, val)
		}
	}

	return ctx
}

// AllowOptionsMW is a helper middleware function that will immediately return a successful request if the method is OPTIONS.
// This makes sure that HTTP OPTIONS calls for CORS functionality are supported.
func AllowOptionsMW() lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		res events.APIGatewayProxyResponse,
		err error,
	) {
		return lres.EmptyRes()
	}
}

// DecodeStandardMW attempts to parse a Json Web Token from the request's "Authorization"
// header. If the Authorization header is missing, or does not contain a valid Json Web Token
// (JWT) then an error message and appropriate HTTP status code will be returned. If the JWT
// is correctly set and contains a StandardClaim then the values from that standard claim
// will be added to the context object for others to use during their processing.
func DecodeStandardMW(next lcom.Handler) lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		res events.APIGatewayProxyResponse,
		err error,
	) {
		mapClaims, httpStatus, err := ljwt.ExtractJWT(req.Headers)
		if err != nil {
			return lres.StatusAndErrorRes(httpStatus, err)
		}

		var standardClaims jwt.StandardClaims
		err = ljwt.ExtractStandard(mapClaims, &standardClaims)
		if err != nil {
			return lres.StatusAndErrorRes(http.StatusInternalServerError, err)
		}

		ctx = context.WithValue(ctx, lcom.JWTClaimAudienceKey, standardClaims.Audience)
		ctx = context.WithValue(ctx, lcom.JWTClaimExpiresAtKey, standardClaims.ExpiresAt)
		ctx = context.WithValue(ctx, lcom.JWTClaimIDKey, standardClaims.Id)
		ctx = context.WithValue(ctx, lcom.JWTClaimIssuedAtKey, standardClaims.IssuedAt)
		ctx = context.WithValue(ctx, lcom.JWTClaimIssuerKey, standardClaims.Issuer)
		ctx = context.WithValue(ctx, lcom.JWTClaimNotBeforeKey, standardClaims.NotBefore)
		ctx = context.WithValue(ctx, lcom.JWTClaimSubjectKey, standardClaims.Subject)

		return next(ctx, req)
	}
}

// DecodeExpandedMW attempts to parse a Json Web Token from the request's "Authorization"
// header. If the Authorization header is missing, or does not contain a valid Json Web Token
// (JWT) then an error message and appropriate HTTP status code will be returned. If the JWT
// is correctly set and contains an instance of ExpandedClaims then the values from
// that standard claim will be added to the context object for others to use during their processing.
func DecodeExpandedMW(next lcom.Handler) lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		res events.APIGatewayProxyResponse,
		err error,
	) {
		mapClaims, httpStatus, err := ljwt.ExtractJWT(req.Headers)
		if err != nil {
			return lres.StatusAndErrorRes(httpStatus, err)
		}

		var extendedClaims ljwt.ExpandedClaims
		err = ljwt.ExtractCustom(mapClaims, &extendedClaims)
		if err != nil {
			return lres.StatusAndErrorRes(http.StatusInternalServerError, err)
		}

		ctx = context.WithValue(ctx, lcom.JWTClaimAudienceKey, extendedClaims.Audience)
		ctx = context.WithValue(ctx, lcom.JWTClaimEmailKey, extendedClaims.Email)
		ctx = context.WithValue(ctx, lcom.JWTClaimExpiresAtKey, extendedClaims.ExpiresAt)
		ctx = context.WithValue(ctx, lcom.JWTClaimFirstNameKey, extendedClaims.FirstName)
		ctx = context.WithValue(ctx, lcom.JWTClaimFullNameKey, extendedClaims.FullName)
		ctx = context.WithValue(ctx, lcom.JWTClaimIDKey, extendedClaims.ID)
		ctx = context.WithValue(ctx, lcom.JWTClaimIssuedAtKey, extendedClaims.IssuedAt)
		ctx = context.WithValue(ctx, lcom.JWTClaimIssuerKey, extendedClaims.Issuer)
		ctx = context.WithValue(ctx, lcom.JWTClaimLevelKey, extendedClaims.Level)
		ctx = context.WithValue(ctx, lcom.JWTClaimNotBeforeKey, extendedClaims.NotBefore)
		ctx = context.WithValue(ctx, lcom.JWTClaimSubjectKey, extendedClaims.Subject)
		ctx = context.WithValue(ctx, lcom.JWTClaimUserTypeKey, extendedClaims.UserType)

		return next(ctx, req)
	}
}
