package serving

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/docker/libkv/store"
	"github.com/rcrowley/go-metrics"
	"github.com/seanbit/gokit/validate"
	"github.com/sirupsen/logrus"
	"github.com/smallnest/rpcx/client"
	rpcxLog "github.com/smallnest/rpcx/log"
	"github.com/smallnest/rpcx/protocol"
	"github.com/smallnest/rpcx/server"
	"github.com/smallnest/rpcx/serverplugin"
	"math"
	"os"
	"os/signal"
	"time"
)

type RpcConfig struct {
	RunMode 				string			`json:"-" validate:"required,oneof=debug test release"`
	RpcPort               	int				`json:"-"`
	RpcPerSecondConnIdle  	int64			`json:"rpc_per_second_conn_idle" validate:"required,gte=1"`
	ReadTimeout           	time.Duration	`json:"read_timeout" validate:"required,gte=1"`
	WriteTimeout          	time.Duration	`json:"write_timeout" validate:"required,gte=1"`
	// token
	TokenAuth				bool			`json:"token_auth"`
	Token                   *TokenConfig	`json:"token"`
	// tls
	TlsAuth 				bool       		`json:"tls_auth"`
	Tls     				*TlsConfig 		`json:"-"`
	// whiteList
	WhitelistAuth 			bool     		`json:"whitelist_auth"`
	Whitelist     			[]string 		`json:"whitelist"`
	// etcd
	Registry				*EtcdRegistry	`json:"-"`
}

type TokenConfig struct {
	TokenSecret      		string        	`json:"token_secret" validate:"required,gte=1"`
	TokenIssuer      		string        	`json:"token_issuer" validate:"required,gte=1"`
}

type TlsConfig struct {
	CACert       			string 			`json:"ca_cert" validate:"required"`
	CACommonName 			string 			`json:"ca_common_name" validate:"required"`
	ServerCert   			string 			`json:"server_cert" validate:"required"`
	ServerKey    			string 			`json:"server_key" validate:"required"`
}

type EtcdRegistry struct {
	EtcdEndPoints 			[]string		`json:"etcd_end_points" validate:"required,gte=1,dive,tcp_addr"`
	EtcdRpcBasePath 		string			`json:"etcd_rpc_base_path" validate:"required,gte=1"`
	EtcdRpcUserName 		string			`json:"etcd_rpc_username" validate:"required,gte=1"`
	EtcdRpcPassword 		string			`json:"etcd_rpc_password" validate:"required,gte=1"`
}

type Registry struct {
	Name string
	Rcvr interface{}
	Metadata string
}

var (
	log *logrus.Entry
	_config      RpcConfig
	_rpc_testing bool = false
)

/**
 * 启动 服务server
 * registerFunc 服务注册回调函数
 */
func Serve(config RpcConfig, logger logrus.FieldLogger, services []Registry, async bool) {
	if logger == nil {
		logger = logrus.New()
	}
	log = logger.WithField("stage", "serving")
	rpcxLog.SetLogger(log)

	// config validate
	configValidate(config)
	_config = config

	// server
	var s *server.Server
	if config.TlsAuth == false {
		s = server.NewServer(server.WithReadTimeout(config.ReadTimeout), server.WithWriteTimeout(config.WriteTimeout))
	} else {
		//cert, err := tls.LoadX509KeyPair(_config.ServerPemPath, _config.ServerKeyPath)
		cert, err := tls.X509KeyPair([]byte(config.Tls.ServerCert), []byte(config.Tls.ServerKey))
		if err != nil {
			log.Fatal(err)
			return
		}
		certPool := x509.NewCertPool()
		ok := certPool.AppendCertsFromPEM([]byte(_config.Tls.CACert))
		if !ok {
			panic("failed to parse root certificate")
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    certPool,
		}
		s = server.NewServer(server.WithTLSConfig(tlsConfig), server.WithReadTimeout(config.ReadTimeout), server.WithWriteTimeout(config.WriteTimeout))
	}

	address := fmt.Sprintf(":%d", _config.RpcPort)
	registerPlugins(s, address)
	for _, registry := range services {
		if err := s.RegisterName(registry.Name, registry.Rcvr, registry.Metadata); err != nil {
			log.Fatal(err)
		}
	}
	switch async {
	case true:
		go func() {
			err := s.Serve("tcp", address)
			if err != nil {
				log.Fatalf("server start error : %v", err)
			}
		}()
	case false:
		err := s.Serve("tcp", address)
		if err != nil {
			log.Fatalf("server start error : %v", err)
		}
		// signal
		quit := make(chan os.Signal)
		signal.Notify(quit, os.Interrupt)
		<- quit
		log.Warn("Shutdown Server ...")
	}
}

func configValidate(config RpcConfig)  {
	if err := validate.ValidateParameter(config); err != nil {
		log.Fatal(err)
	}
	if config.Registry == nil {
		log.Fatal("registry for rpc could not be nil.")
	}
	if err := validate.ValidateParameter(config.Registry); err != nil {
		log.Fatal(err)
	}
	if config.TlsAuth {
		if config.Tls == nil {
			log.Fatal("server rpc start error : secret is nil")
		}
		if err := validate.ValidateParameter(config.Tls); err != nil {
			log.Fatal(err)
		}
	}
}

func registerPlugins(s *server.Server, address string)  {
	// logger
	s.Plugins.Add(ServerLogger)
	// token auth
	if _config.TokenAuth {
		s.AuthFunc = serverAuth
	}
	// whitelist auth
	if _config.WhitelistAuth {
		var wl = make(map[string]bool)
		for _, ip := range _config.Whitelist {
			wl[ip] = true
		}
		s.Plugins.Add(serverplugin.WhitelistPlugin{
			Whitelist:     wl,
			WhitelistMask: nil,
		})
	}
	// etcd register
	RegisterPluginEtcd(s, address)
	// ratelimit
	RegisterPluginRateLimit(s)
}

/**
 * 注册插件，Etcd注册中心，服务发现
 */
func RegisterPluginEtcd(s *server.Server, serviceAddr string)  {
	if _rpc_testing == true {
		plugin := client.InprocessClient
		s.Plugins.Add(plugin)
		return
	}
	plugin := &serverplugin.EtcdV3RegisterPlugin{
		ServiceAddress: "tcp@" + serviceAddr,
		EtcdServers:    _config.Registry.EtcdEndPoints,
		BasePath:       _config.Registry.EtcdRpcBasePath,
		Metrics:        metrics.NewRegistry(),
		Services:       nil,
		UpdateInterval: time.Minute,
		Options:        &store.Config{
			ClientTLS:         nil,
			TLS:               nil,
			ConnectionTimeout: 3 * time.Minute,
			Bucket:            "",
			PersistConnection: false,
			Username:          _config.Registry.EtcdRpcUserName,
			Password:          _config.Registry.EtcdRpcPassword,
		},
	}
	err := plugin.Start()
	if err != nil {
		log.Fatal(err)
	}
	s.Plugins.Add(plugin)
}

/**
 * 注册插件，限流器，限制客户端连接数
 */
func RegisterPluginRateLimit(s *server.Server)  {
	var fillSpeed float64 = 1.0 / float64(_config.RpcPerSecondConnIdle)
	fillInterval := time.Duration(fillSpeed * math.Pow(10, 9))
	plugin := serverplugin.NewRateLimitingPlugin(fillInterval, _config.RpcPerSecondConnIdle)
	s.Plugins.Add(plugin)
}

/**
 * server token auth
 */
func serverAuth(ctx context.Context, req *protocol.Message, token string) error {
	if trace, err := parseToken(token, _config.Token.TokenSecret, _config.Token.TokenIssuer); err != nil {
		return err
	} else if trace.TraceId <= 0 {
		return errors.New("serving rpc resp error: invalid traceId in context")
	}
	return nil
}





