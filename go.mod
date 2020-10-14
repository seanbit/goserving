module github.com/seanbit/goserving

go 1.14

require (
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/docker/libkv v0.2.1
	github.com/rcrowley/go-metrics v0.0.0-20200313005456-10cdbea86bc0
	github.com/sean-tech/gokit v1.2.9 // indirect
	github.com/seanbit/gokit v1.0.1
	github.com/sirupsen/logrus v1.2.0
	github.com/smallnest/rpcx/v5 v5.7.6
)

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0
