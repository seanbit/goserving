package serving

import (
	"errors"
	"github.com/dgrijalva/jwt-go"
	"time"
)

type tokenInfo struct {
	*Trace
	jwt.StandardClaims
}

/**
 * 生成token
 */
func generateToken(trace *Trace, tokenSecret string, tokenIssuer string, tokenExpiresTime time.Duration) (string, error) {
	expireTime := time.Now().Add(tokenExpiresTime)
	iat := time.Now().Unix()
	//jti := _httpConfig.IdWorker.GetId()
	c := tokenInfo{
		Trace: trace,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: 	expireTime.Unix(),
			Issuer:    	tokenIssuer,
			//Id:strconv.FormatInt(jti, 10),
			IssuedAt:	iat,
			NotBefore: 	iat,
			Subject:	"rpc_client",
		},
	}
	tokenClaims := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	token, err := tokenClaims.SignedString([]byte(tokenSecret))
	if err != nil {
		return "", err
	}
	return token, nil
}

/**
 * 解析token
 */
func parseToken(token string, tokenSecret string, tokenIssuer string) (*Trace, error) {
	if token == "" || len(token) == 0 {
		return nil, errors.New("rpc token parse failed : token is nil")
	}
	tokenClaims, err := jwt.ParseWithClaims(token, &tokenInfo{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(tokenSecret), nil
	})
	if err != nil {
		return nil, err
	}
	if tokenClaims == nil {
		return nil, errors.New("rpc token parse failed : token claims is nil")
	}
	if !tokenClaims.Valid {
		return nil, errors.New("rpc token parse failed : token is not valid")
	}
	claims, ok := tokenClaims.Claims.(*tokenInfo)
	if !ok {
		return nil, errors.New("rpc token parse failed : token type error")
	}
	if claims.Issuer != tokenIssuer {
		return nil, errors.New("rpc token parse failed : token issuer validate wrong")
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, errors.New("rpc token parse failed : token after expires time")
	}
	return claims.Trace, nil
}

func checkToken(token string, tokenSecret string, tokenIssuer string) error {
	if _, err := parseToken(token, tokenSecret, tokenIssuer); err != nil {
		return err
	}
	return nil
}