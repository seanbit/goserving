module github.com/seanbit/goserving

go 1.14

require (
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/docker/libkv v0.2.1
	github.com/rcrowley/go-metrics v0.0.0-20200313005456-10cdbea86bc0
	github.com/seanbit/gokit v1.0.4
	github.com/sirupsen/logrus v1.2.0
	github.com/smallnest/rpcx v0.0.0-20200924044220-f2cdd4dea15a
)

replace google.golang.org/grpc => google.golang.org/grpc v1.29.0
