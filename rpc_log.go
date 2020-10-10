package serving

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/seanbit/gokit/foundation"
	"github.com/sirupsen/logrus"
	"github.com/smallnest/rpcx/protocol"
	"github.com/smallnest/rpcx/share"
	"net"
	"reflect"
	"sync"
	"unicode"
	"unicode/utf8"
)

type clientLogger struct {}
var ClientLogger = &clientLogger{}

func (this *clientLogger) DoPreCall(ctx context.Context, servicePath, serviceMethod string, args interface{}) error {
	return nil
}

// PostCallPlugin is invoked after the client calls a server.
func (this *clientLogger) DoPostCall(ctx context.Context, servicePath, serviceMethod string, args interface{}, reply interface{}, err error) error {
	var traceId uint64 = 0
	var userName string = ""
	if trace := GetTrace(ctx); trace != nil {
		traceId = trace.TraceId
		userName = trace.UserName
	}

	var argsInfo string = ""; var replyInfo string = ""
	if argsBytes, err := json.Marshal(args); err == nil {
		argsInfo = string(argsBytes)
	} else {
		argsInfo = fmt.Sprintf("%+v", args)
	}
	if replyBytes, err := json.Marshal(reply); err == nil {
		replyInfo = string(replyBytes)
	} else {
		replyInfo = fmt.Sprintf("%+v", reply)
	}
	calllog := log.WithFields(logrus.Fields{"traceId":traceId, "userName":userName, "callPath":servicePath, "callMethod":serviceMethod, "callArgs":argsInfo, "callReply":replyInfo})
	if err != nil {
		if e, ok := err.(foundation.Error); ok {
			calllog.Infof("service call failed:%d %s", e.Code(), e.Msg())
		} else {
			calllog.Infof("service call failed:%s", err.Error())
		}
		if err.Error() != "" {
			calllog.Error(err.Error())
		}
		return nil
	}
	calllog.Infof("service call success")
	return nil
}

// ConnCreatedPlugin is invoked when the client connection has created.
func (this *clientLogger) ConnCreated(conn net.Conn) (net.Conn, error) {
	return conn, nil
}

// ClientConnectedPlugin is invoked when the client has connected the server.
func (this *clientLogger) ClientConnected(conn net.Conn) (net.Conn, error) {
	return conn, nil
}

// ClientConnectionClosePlugin is invoked when the connection is closing.
func (this *clientLogger) ClientConnectionClose(net.Conn) error {
	return nil
}

// ClientBeforeEncodePlugin is invoked when the message is encoded and sent.
func (this *clientLogger) ClientBeforeEncode(*protocol.Message) error {
	return nil
}

// ClientAfterDecodePlugin is invoked when the message is decoded.
func (this *clientLogger) ClientAfterDecode(*protocol.Message) error {
	return nil
}



type serverlogger struct {
	serviceMapMu sync.RWMutex
	serviceMap   map[string]*service
}
var ServerLogger = &serverlogger{sync.RWMutex{},
	make(map[string]*service),
}

func (this *serverlogger) Register(name string, rcvr interface{}, metadata string) error {
	this.register(rcvr, name, true)
	return nil
}

func (this *serverlogger) Unregister(name string) error {
	this.UnRegister(name)
	return nil
}

func (this *serverlogger) PostReadRequest(ctx context.Context, r *protocol.Message, e error) error {
	this.logPrint("PostReadRequest", ctx, r, MsgTypeReq, e)
	return nil
}

func (this *serverlogger)PreHandleRequest(ctx context.Context, r *protocol.Message) error {
	return nil
}
func (this *serverlogger) PreWriteResponse(ctx context.Context, req *protocol.Message, resp *protocol.Message) error {
	return nil
}

func (this *serverlogger) PostWriteResponse(ctx context.Context, req *protocol.Message, resp *protocol.Message, e error) error {
	this.logPrint("PostWriteResponse", ctx, resp, MsgTypeResp, e)
	return nil
}

func (this *serverlogger) PreWriteRequest(ctx context.Context) error {
	return nil
}
func (this *serverlogger) PostWriteRequest(ctx context.Context, r *protocol.Message, e error) error {
	return nil
}

func (this *serverlogger) logPrint(prefix string, ctx context.Context, msg *protocol.Message, msgType MsgType, err error)  {
	var traceId uint64 = 0
	var userName string = ""
	if trace := GetTrace(ctx); trace != nil {
		traceId = trace.TraceId
		userName = trace.UserName
	}
	resplog := log.WithFields(logrus.Fields{"traceId":traceId, "userName":userName, "msgPath":msg.ServicePath, "msgMethod":msg.ServiceMethod})
	if err != nil {
		if e, ok := err.(foundation.Error); ok {
			resplog.Infof("service resp error:%d %s", e.Code(), e.Msg())
		} else {
			resplog.Infof("service resp error:%s", err.Error())
		}
		if err.Error() != "" {
			resplog.Error(err.Error())
		}
		return
	}
	data := this.paylodConvert(ctx, msg, msgType)
	log.WithFields(logrus.Fields{"metadata":msg.Metadata, "payload":data}).Info("service resp success")
}



// Precompute the reflect type for error. Can't use error directly
// because Typeof takes an empty interface value. This is annoying.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()
// Precompute the reflect type for context.
var typeOfContext = reflect.TypeOf((*context.Context)(nil)).Elem()

type methodType struct {
	sync.Mutex // protects counters
	method     reflect.Method
	ArgType    reflect.Type
	ReplyType  reflect.Type
	// numCalls   uint
}

type functionType struct {
	sync.Mutex // protects counters
	fn         reflect.Value
	ArgType    reflect.Type
	ReplyType  reflect.Type
}

type service struct {
	name     string                   // name of service
	rcvr     reflect.Value            // receiver of methods for the service
	typ      reflect.Type             // type of the receiver
	method   map[string]*methodType   // registered methods
	function map[string]*functionType // registered functions
}


func isExported(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}

func (this *serverlogger) register(rcvr interface{}, name string, useName bool) (string, error) {
	service := new(service)
	service.typ = reflect.TypeOf(rcvr)
	service.rcvr = reflect.ValueOf(rcvr)
	sname := reflect.Indirect(service.rcvr).Type().Name() // Type
	if useName {
		sname = name
	}
	if sname == "" {
		errorStr := "rpcx.Register: no service name for type " + service.typ.String()
		log.Error(errorStr)
		return sname, errors.New(errorStr)
	}
	if !useName && !isExported(sname) {
		errorStr := "rpcx.Register: type " + sname + " is not exported"
		log.Error(errorStr)
		return sname, errors.New(errorStr)
	}
	service.name = sname

	// Install the methods
	service.method = suitableMethods(service.typ, true)

	if len(service.method) == 0 {
		var errorStr string

		// To help the user, see if a pointer receiver would work.
		method := suitableMethods(reflect.PtrTo(service.typ), false)
		if len(method) != 0 {
			errorStr = "rpcx.Register: type " + sname + " has no exported methods of suitable type (hint: pass a pointer to value of that type)"
		} else {
			errorStr = "rpcx.Register: type " + sname + " has no exported methods of suitable type"
		}
		log.Error(errorStr)
		return sname, errors.New(errorStr)
	}
	this.serviceMap[service.name] = service
	return sname, nil
}

// suitableMethods returns suitable Rpc methods of typ, it will report
// error using log if reportErr is true.
func suitableMethods(typ reflect.Type, reportErr bool) map[string]*methodType {
	methods := make(map[string]*methodType)
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		mtype := method.Type
		mname := method.Name
		// Method must be exported.
		if method.PkgPath != "" {
			continue
		}
		// Method needs four ins: receiver, context.Context, *args, *reply.
		if mtype.NumIn() != 4 {
			if reportErr {
				log.Debug("method ", mname, " has wrong number of ins:", mtype.NumIn())
			}
			continue
		}
		// First arg must be context.Context
		ctxType := mtype.In(1)
		if !ctxType.Implements(typeOfContext) {
			if reportErr {
				log.Debug("method ", mname, " must use context.Context as the first parameter")
			}
			continue
		}

		// Second arg need not be a pointer.
		argType := mtype.In(2)
		if !isExportedOrBuiltinType(argType) {
			if reportErr {
				log.Info(mname, " parameter type not exported: ", argType)
			}
			continue
		}
		// Third arg must be a pointer.
		replyType := mtype.In(3)
		if replyType.Kind() != reflect.Ptr {
			if reportErr {
				log.Info("method", mname, " reply type not a pointer:", replyType)
			}
			continue
		}
		// Reply type must be exported.
		if !isExportedOrBuiltinType(replyType) {
			if reportErr {
				log.Info("method", mname, " reply type not exported:", replyType)
			}
			continue
		}
		// Method needs one out.
		if mtype.NumOut() != 1 {
			if reportErr {
				log.Info("method", mname, " has wrong number of outs:", mtype.NumOut())
			}
			continue
		}
		// The return type of the method must be error.
		if returnType := mtype.Out(0); returnType != typeOfError {
			if reportErr {
				log.Info("method", mname, " returns ", returnType.String(), " not error")
			}
			continue
		}
		methods[mname] = &methodType{method: method, ArgType: argType, ReplyType: replyType}

		argsReplyPools.Init(argType)
		argsReplyPools.Init(replyType)
	}
	return methods
}

var UsePool bool

// Reset defines Reset method for pooled object.
type Reset interface {
	Reset()
}

var argsReplyPools = &typePools{
	pools: make(map[reflect.Type]*sync.Pool),
	New: func(t reflect.Type) interface{} {
		var argv reflect.Value

		if t.Kind() == reflect.Ptr { // reply must be ptr
			argv = reflect.New(t.Elem())
		} else {
			argv = reflect.New(t)
		}

		return argv.Interface()
	},
}

type typePools struct {
	mu    sync.RWMutex
	pools map[reflect.Type]*sync.Pool
	New   func(t reflect.Type) interface{}
}

func (p *typePools) Init(t reflect.Type) {
	tp := &sync.Pool{}
	tp.New = func() interface{} {
		return p.New(t)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pools[t] = tp
}

func (p *typePools) Put(t reflect.Type, x interface{}) {
	if !UsePool {
		return
	}
	if o, ok := x.(Reset); ok {
		o.Reset()
	}

	p.mu.RLock()
	pool := p.pools[t]
	p.mu.RUnlock()
	pool.Put(x)
}

func (p *typePools) Get(t reflect.Type) interface{} {
	if !UsePool {
		return p.New(t)
	}
	p.mu.RLock()
	pool := p.pools[t]
	p.mu.RUnlock()

	return pool.Get()
}



// UnregisterAll unregisters all services.
// You can call this method when you want to shutdown/upgrade this node.
func (this *serverlogger) UnregisterAll() {
	for k := range this.serviceMap {
		delete(this.serviceMap, k)
	}
}

func (this *serverlogger) UnRegister(name string) {
	delete(this.serviceMap, name)
}

type MsgType int

const (
	MsgTypeReq 	= 0
	MsgTypeResp = 1
)

func (this *serverlogger) paylodConvert(ctx context.Context, msg *protocol.Message, msgType MsgType) interface{} {
	serviceName := msg.ServicePath
	methodName := msg.ServiceMethod

	this.serviceMapMu.RLock()
	service := this.serviceMap[serviceName]
	this.serviceMapMu.RUnlock()
	if service == nil {
		return nil
	}

	dataType := getDataType(service, methodName, msgType)
	var data = argsReplyPools.Get(dataType)
	codec := share.Codecs[msg.SerializeType()]
	if codec == nil {
		return nil
	}

	if err := codec.Decode(msg.Payload, data); err != nil {
		return nil
	}
	argsReplyPools.Put(dataType, data)


	fmt.Println(data)
	return data
}

func getDataType(service *service, methodName string, msgType MsgType) reflect.Type {
	if mtype := service.method[methodName]; mtype != nil {
		switch msgType {
		case MsgTypeReq:
			return mtype.ArgType
		case MsgTypeResp:
			return mtype.ReplyType
		}
	}
	if service.function[methodName] == nil {
		return nil
	}
	if mtype := service.function[methodName]; mtype != nil {
		switch msgType {
		case MsgTypeReq:
			return mtype.ArgType
		case MsgTypeResp:
			return mtype.ReplyType
		}
	}
	return nil
}
