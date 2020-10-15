package serving

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/docker/libkv/store"
	"github.com/smallnest/rpcx/client"
	"github.com/smallnest/rpcx/share"
	"net"
	"sync"
	"time"
)

var (
	clientMap sync.Map
	p2pClientMap sync.Map
)
/**
 * 创建rpc调用客户端，基于Etcd服务发现
 */
func CreateClient(serviceName, serverName string) client.XClient {
	if c, ok := clientMap.Load(serviceName); ok {
		return c.(client.XClient)
	}
	option := createClientOption(serverName)
	xclient := client.NewXClient(serviceName, client.Failover, client.RoundRobin, newDiscovery(serviceName), option)
	xclient.GetPlugins().Add(ClientLogger)
	xclient.GetPlugins().Add(&clientCloser{serviceName: serviceName})
	clientMap.Store(serviceName, xclient)
	return xclient
}

/**
 * 创建rpc调用客户端，基于Etcd服务发现
 */
func CreateP2pClient(serviceName, serverName string, addrs []string) client.XClient {
	if c, ok := p2pClientMap.Load(serviceName); ok {
		return c.(client.XClient)
	}
	option := createClientOption(serverName)
	xclient := client.NewXClient(serviceName, client.Failover, client.RoundRobin, newP2PDiscovery(addrs), option)
	xclient.GetPlugins().Add(ClientLogger)
	xclient.GetPlugins().Add(&clientCloser{serviceName: serviceName})
	p2pClientMap.Store(serviceName, xclient)
	return xclient
}

func createClientOption(serverName string) client.Option {
	option := client.DefaultOption
	option.Heartbeat = true
	option.HeartbeatInterval = time.Second
	option.ConnectTimeout = _config.ReadTimeout
	option.IdleTimeout = _config.ReadTimeout
	if _config.TlsAuth {
		//cert, err := tls.LoadX509KeyPair(_config.ServerPemPath, _config.ServerKeyPath)
		cert, err := tls.X509KeyPair([]byte(_config.Tls.ServerCert), []byte(_config.Tls.ServerKey))
		if err != nil {
			log.Errorf("unable to read cert.pem and cert.key : %s", err.Error())
			return option
		}
		certPool := x509.NewCertPool()
		ok := certPool.AppendCertsFromPEM([]byte(_config.Tls.CACert))
		if !ok {
			log.Errorf("failed to parse root certificate : %s", err.Error())
			return option
		}
		option.TLSConfig = &tls.Config{
			RootCAs:            certPool,
			Certificates:       []tls.Certificate{cert},
			InsecureSkipVerify: false,
			ServerName: serverName + "." + _config.Tls.CACommonName,
		}
	}
	return option
}

func newP2PDiscovery(addrs []string) client.ServiceDiscovery {
	if addrs == nil {
		log.Error("failed to create service Peer2Peer discovery : addrs is nil")
		return nil
	}
	if len(addrs) == 1 {
		return client.NewPeer2PeerDiscovery("tcp@" +addrs[0], "")
	}
	var clientkvs []*client.KVPair
	for _, addr := range addrs {
		clientkvs = append(clientkvs, &client.KVPair{Key: addr})
	}
	return client.NewMultipleServersDiscovery(clientkvs)
}

func newDiscovery(serviceName string) client.ServiceDiscovery {
	var discovery client.ServiceDiscovery
	var options = &store.Config{
		ClientTLS:         nil,
		TLS:               nil,
		ConnectionTimeout: 0,
		Bucket:            "",
		PersistConnection: false,
		Username:          _config.Registry.EtcdRpcUserName,
		Password:          _config.Registry.EtcdRpcPassword,
	}
	if _rpc_testing == true {
		discovery = client.NewInprocessDiscovery()
	} else {
		discovery = client.NewEtcdV3Discovery(_config.Registry.EtcdRpcBasePath, serviceName, _config.Registry.EtcdEndPoints, options)
	}
	return discovery
}

/**
 * client call
 */
func ClientCall(client client.XClient, ctx context.Context, serviceMethod string, args interface{}, reply interface{}) error {
	if ctx, err := clientAuth(client, ctx); err != nil {
		return err
	} else {
		return client.Call(ctx, serviceMethod, args, reply)
	}
}

/**
 * client go
 */
func ClientGo(client client.XClient, ctx context.Context, serviceMethod string, args interface{}, reply interface{}, done chan *client.Call) (*client.Call, error) {
	if ctx, err := clientAuth(client, ctx); err != nil {
		return nil, err
	} else {
		return client.Go(ctx, serviceMethod, args, reply, done)
	}
}

/**
 * call
 */
func Call(serviceName, serverName string, ctx context.Context, serviceMethod string, args interface{}, reply interface{}) error {
	client := CreateClient(serviceName, serverName)
	return ClientCall(client, ctx, serviceMethod, args, reply)
}

/**
 * go
 */
func Go(serviceName, serverName string, ctx context.Context, serviceMethod string, args interface{}, reply interface{}, done chan *client.Call) (*client.Call, error) {
	client := CreateClient(serviceName, serverName)
	return ClientGo(client, ctx, serviceMethod, args, reply, done)
}

/**
 * p2p call
 */
func P2pCall(serviceName, serverName string, addrs []string, ctx context.Context, serviceMethod string, args interface{}, reply interface{}) error {
	client := CreateP2pClient(serviceName, serverName, addrs)
	return ClientCall(client, ctx, serviceMethod, args, reply)
}

/**
 * p2p go
 */
func P2pGo(serviceName, serverName string, addrs []string, ctx context.Context, serviceMethod string, args interface{}, reply interface{}, done chan *client.Call) (*client.Call, error) {
	client := CreateP2pClient(serviceName, serverName, addrs)
	return ClientGo(client, ctx, serviceMethod, args, reply, done)
}

type clientCloser struct {
	serviceName string
}
// ClientConnectionClosePlugin is invoked when the connection is closing.
func (this *clientCloser) ClientConnectionClose(net.Conn) error {
	if _, ok := clientMap.Load(this.serviceName); ok {
		clientMap.Delete(this.serviceName)
	}
	return nil
}

/**
 * client auth
 */
func clientAuth(client client.XClient, ctx context.Context) (context.Context, error) {
	if ctx == nil {
		return nil, errors.New("serving rpc client call error: context is nil")
	}
	trace := GetTrace(ctx)
	if trace == nil {
		return ctx, errors.New("serving rpc client call error: trace in context is nil")
	}
	if _config.TokenAuth {
		var expiresTime = (_config.ReadTimeout + _config.WriteTimeout) * 2
		if token, err := generateToken(trace, _config.Token.TokenSecret, _config.Token.TokenIssuer, expiresTime); err != nil {
			return ctx, err
		} else {
			client.Auth(token)
		}
	}

	ctx = context.WithValue(ctx, share.ReqMetaDataKey, make(map[string]string))
	return ctx, nil
}