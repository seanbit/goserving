package serving

import (
	"context"
)

const (
	key_ctx_trace = "seanbit/serving/key_ctx_trace"
)

type Trace struct {
	TraceId 	uint64		`json:"traceId" validate:"required,gte=1"`
	UserId      uint64      `json:"userId" validate:"required,gte=1"`
	UserName    string      `json:"userName" validate:"required,gte=1"`
	UserRole	string		`json:"userRole" validate:"required,gte=1"`
}

type BindContext interface {
	Set(key string, value interface{})
}

func TraceBind(ctx BindContext, traceId, userId uint64, userName, userRole string) (*Trace, error) {
	trace := &Trace{
		TraceId:  traceId,
		UserId:   userId,
		UserName: userName,
		UserRole: userRole,
	}
	ctx.Set(key_ctx_trace, trace)
	return trace, nil
}

func TraceContext(ctx context.Context, traceId, userId uint64, userName, userRole string) (context.Context, error) {
	trace := &Trace{
		TraceId:  traceId,
		UserId:   userId,
		UserName: userName,
		UserRole: userRole,
	}
	return context.WithValue(ctx, key_ctx_trace, trace), nil
}

/**
 * 信息获取，获取传输链上context绑定的用户请求调用信息
 */
func GetTrace(ctx context.Context) *Trace {
	obj := ctx.Value(key_ctx_trace)
	if trace, ok := obj.(*Trace); ok {
		return  trace
	}
	return nil
}

/**
 * 信息校验，token绑定的用户信息同参数传入信息校验，信息不一致说明恶意用户传他人数据渗透
 */
func ConformTraceUser(ctx context.Context, userId uint64, userName string) bool {
	trace := GetTrace(ctx)
	if trace == nil {
		return false
	}
	if trace.UserId != userId || trace.UserName != userName {
		return false
	}
	return true
}





