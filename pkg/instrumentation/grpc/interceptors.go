// Package grpc provides gRPC instrumentation.
package grpc

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/hyp3rd/observe/pkg/config"
)

// Interceptors bundles server and client interceptors for gRPC instrumentation.
type Interceptors struct {
	unaryServer grpc.UnaryServerInterceptor
	unaryClient grpc.UnaryClientInterceptor
}

// NewInterceptors constructs gRPC interceptors backed by the supplied tracer provider.
func NewInterceptors(tp trace.TracerProvider, cfg config.GRPCInstrumentationConfig) Interceptors {
	tracer := tp.Tracer("observe/grpc")
	allowlist := buildAllowlist(cfg.MetadataAllowlist)

	return Interceptors{
		unaryServer: newUnaryServerInterceptor(tracer, allowlist),
		unaryClient: newUnaryClientInterceptor(tracer, allowlist),
	}
}

// UnaryServer returns the configured unary server interceptor.
func (i Interceptors) UnaryServer() grpc.UnaryServerInterceptor {
	return i.unaryServer
}

// UnaryClient returns the configured unary client interceptor.
func (i Interceptors) UnaryClient() grpc.UnaryClientInterceptor {
	return i.unaryClient
}

func newUnaryServerInterceptor(tracer trace.Tracer, allowlist map[string]struct{}) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		service, method := splitFullMethod(info.FullMethod)

		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		attrs := []attribute.KeyValue{
			semconv.RPCSystemGRPC,
			semconv.RPCServiceKey.String(service),
			semconv.RPCMethodKey.String(method),
		}

		if md, ok := metadata.FromIncomingContext(ctx); ok {
			attrs = append(attrs, metadataAttrs(md, allowlist)...)
		}

		span.SetAttributes(attrs...)

		resp, err := handler(ctx, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return resp, err
	}
}

func newUnaryClientInterceptor(tracer trace.Tracer, allowlist map[string]struct{}) grpc.UnaryClientInterceptor {
	return func(ctx context.Context,
		method string, req,
		reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		service, rpcMethod := splitFullMethod(method)

		ctx, span := tracer.Start(ctx, method, trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()

		attrs := []attribute.KeyValue{
			semconv.RPCSystemGRPC,
			semconv.RPCServiceKey.String(service),
			semconv.RPCMethodKey.String(rpcMethod),
		}

		if md, ok := metadata.FromOutgoingContext(ctx); ok {
			attrs = append(attrs, metadataAttrs(md, allowlist)...)
		}

		span.SetAttributes(attrs...)

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			return err
		}

		span.SetStatus(codes.Ok, "")

		return nil
	}
}

func buildAllowlist(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}

	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}

		out[key] = struct{}{}
	}

	return out
}

func metadataAttrs(md metadata.MD, allowlist map[string]struct{}) []attribute.KeyValue {
	if len(md) == 0 || len(allowlist) == 0 {
		return nil
	}

	attrs := make([]attribute.KeyValue, 0, len(allowlist))
	for key := range allowlist {
		values := md.Get(key)
		if len(values) == 0 {
			continue
		}

		attrKey := attribute.Key("rpc.metadata." + key)
		attrs = append(attrs, attrKey.String(strings.Join(values, ",")))
	}

	return attrs
}

func splitFullMethod(full string) (service, method string) {
	full = strings.TrimPrefix(full, "/")
	if full == "" {
		service = "unknown"
		method = "unknown"

		return service, method
	}

	parts := strings.Split(full, "/")
	if len(parts) != 2 {
		service = full
		method = "unknown"

		return service, method
	}

	service = parts[0]
	method = parts[1]

	return service, method
}
