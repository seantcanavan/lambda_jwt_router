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
		format := "[%s] [%s %s] [%d]%s"
		level := "INF"
		var code int
		var extra string

		res, err = next(ctx, req)
		if err != nil {
			level = "ERR"
			code = http.StatusInternalServerError
			extra = " " + err.Error()
		} else {
			code = res.StatusCode
			if code >= 400 {
				level = "ERR"
			}
		}

		if code > 399 {
			resJSON, _ := json.Marshal(res)
			log.Error().
				Interface(lcom.LambdaContextIDKey, ctx.Value(lcom.LambdaContextIDKey)).
				Interface(lcom.LambdaContextMethodKey, ctx.Value(lcom.LambdaContextMethodKey)).
				Interface(lcom.LambdaContextMultiParamsKey, ctx.Value(lcom.LambdaContextMultiParamsKey)).
				Interface(lcom.LambdaContextPathKey, ctx.Value(lcom.LambdaContextPathKey)).
				Interface(lcom.LambdaContextPathParamsKey, ctx.Value(lcom.LambdaContextPathParamsKey)).
				Interface(lcom.LambdaContextQueryParamsKey, ctx.Value(lcom.LambdaContextQueryParamsKey)).
				Interface(lcom.LambdaContextRequestIDKey, ctx.Value(lcom.LambdaContextRequestIDKey)).
				Interface(lcom.LambdaContextUserIDKey, ctx.Value(lcom.LambdaContextUserIDKey)).
				Interface(lcom.LambdaContextUserTypeKey, ctx.Value(lcom.LambdaContextUserTypeKey)).
				RawJSON("res", resJSON).
				Msg("error encountered")
		}

		log.Printf(format, level, req.HTTPMethod, req.Path, code, extra)

		return res, err
	}
}

// InjectLambdaContextMW with do exactly that - inject all appropriate lambda values into the local
// context so that other users down the line can query the context for things like HTTP method or Path
func InjectLambdaContextMW(next lcom.Handler) lcom.Handler {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (
		res events.APIGatewayProxyResponse,
		err error,
	) {

		lambdaParams := &lcom.LambdaParams{}
		err = lreq.UnmarshalReq(req, true, lambdaParams)
		if err != nil {
			return lres.ErrorRes(err)
		}

		ctx = context.WithValue(ctx, lcom.LambdaContextIDKey, lambdaParams.GetID())
		ctx = context.WithValue(ctx, lcom.LambdaContextMethodKey, req.HTTPMethod)
		ctx = context.WithValue(ctx, lcom.LambdaContextMultiParamsKey, req.MultiValueQueryStringParameters)
		ctx = context.WithValue(ctx, lcom.LambdaContextPathKey, req.Path)
		ctx = context.WithValue(ctx, lcom.LambdaContextPathParamsKey, req.PathParameters)
		ctx = context.WithValue(ctx, lcom.LambdaContextQueryParamsKey, req.QueryStringParameters)
		ctx = context.WithValue(ctx, lcom.LambdaContextRequestIDKey, req.RequestContext.RequestID)
		ctx = context.WithValue(ctx, lcom.LambdaContextUserIDKey, lambdaParams.GetUserID())
		ctx = context.WithValue(ctx, lcom.LambdaContextUserTypeKey, lambdaParams.GetUserType())

		return next(ctx, req)
	}
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
