package client

import (
	"context"
	"log"
	"time"

	"github.com/damianoneill/net/v2/netconf/common"

	"github.com/imdario/mergo"
	"golang.org/x/crypto/ssh"
)

// unique type to prevent assignment.
type clientEventContextKey struct{}

// ContextClientTrace returns the Trace associated with the
// provided context. If none, it returns nil.
func ContextClientTrace(ctx context.Context) *ClientTrace {
	trace, _ := ctx.Value(clientEventContextKey{}).(*ClientTrace)
	if trace == nil {
		trace = NoOpLoggingHooks
	} else {
		_ = mergo.Merge(trace, NoOpLoggingHooks) // nolint: gosec, errcheck
	}
	return trace
}

// WithClientTrace returns a new context based on the provided parent
// ctx. Netconf client requests made with the returned context will use
// the provided trace hooks
func WithClientTrace(ctx context.Context, trace *ClientTrace) context.Context {

	// old := ContextClientTrace(ctx)
	// trace.compose(old)

	ctx = context.WithValue(ctx, clientEventContextKey{}, trace)
	return ctx
}

// ClientTrace defines a structure for handling trace events
type ClientTrace struct {
	// ConnectStart is called when starting to connect to a remote server.
	ConnectStart func(clientConfig *ssh.ClientConfig, target string)

	// ConnectDone is called when the transport connection attempt completes, with err indicating
	// whether it was successful.
	ConnectDone func(clientConfig *ssh.ClientConfig, target string, err error, d time.Duration)

	// HelloDone is called when the hello message has been received from the server.
	HelloDone func(msg *common.HelloMessage)

	// ConnectionClosed is called after a transport connection has been closed, with
	// err indicating any error condition.
	ConnectionClosed func(target string, err error)

	// ReadStart is called before a read from the underlying transport.
	ReadStart func(buf []byte)

	// ReadDone is called after a read from the underlying transport.
	ReadDone func(buf []byte, c int, err error, d time.Duration)

	// WriteStart is called before a write to the underlying transport.
	WriteStart func(buf []byte)

	// WriteDone is called after a write to the underlying transport.
	WriteDone func(buf []byte, c int, err error, d time.Duration)

	// Error is called after an error condition has been detected.
	Error func(context, target string, err error)

	// NotificationReceived is called when a notification has been received.
	NotificationReceived func(m *common.Notification)

	// NotificationDropped is called when a notification is dropped because the reader is not ready.
	NotificationDropped func(m *common.Notification)

	// ExecuteStart is called before the execution of an rpc request.
	ExecuteStart func(req common.Request, async bool)

	// ExecuteDone is called after the execution of an rpc request.
	ExecuteDone func(req common.Request, async bool, res *common.RPCReply, err error, d time.Duration)
}

// DefaultLoggingHooks provides a default logging hook to report errors.
var DefaultLoggingHooks = &ClientTrace{
	Error: func(context, target string, err error) {
		log.Printf("NETCONF-Error context:%s target:%s err:%v\n", context, target, err)
	},
}

// MetricLoggingHooks provides a set of hooks that will log network metrics.
var MetricLoggingHooks = &ClientTrace{
	ConnectDone: func(clientConfig *ssh.ClientConfig, target string, err error, d time.Duration) {
		log.Printf("NETCONF-ConnectDone target:%s config:%v err:%v took:%dms\n", target, clientConfig, err, d.Milliseconds())
	},
	ReadDone: func(p []byte, c int, err error, d time.Duration) {
		log.Printf("NETCONF-ReadDone len:%d err:%v took:%dms\n", c, err, d.Milliseconds())
	},
	WriteDone: func(p []byte, c int, err error, d time.Duration) {
		log.Printf("NETCONF-WriteDone len:%d err:%v took:%dms\n", c, err, d.Milliseconds())
	},

	Error: DefaultLoggingHooks.Error,

	ExecuteDone: func(req common.Request, async bool, res *common.RPCReply, err error, d time.Duration) {
		log.Printf("NETCONF-ExecuteDone async:%v err:%v took:%dms\n", async, err, d.Milliseconds())
	},
}

// DiagnosticLoggingHooks provides a set of default diagnostic hooks
var DiagnosticLoggingHooks = &ClientTrace{
	ConnectStart: func(clientConfig *ssh.ClientConfig, target string) {
		log.Printf("NETCONF-ConnectStart target:%s config:%v\n", target, clientConfig)
	},
	ConnectDone: MetricLoggingHooks.ConnectDone,
	ConnectionClosed: func(target string, err error) {
		log.Printf("NETCONF-ConnectionClosed target:%s err:%v\n", target, err)
	},
	ReadStart: func(p []byte) {
		log.Printf("NETCONF-ReadStart capacity:%d\n", len(p))
	},
	ReadDone: MetricLoggingHooks.ReadDone,
	WriteStart: func(p []byte) {
		log.Printf("NETCONF-WriteStart len:%d\n", len(p))
	},
	WriteDone: MetricLoggingHooks.WriteDone,

	Error: DefaultLoggingHooks.Error,

	NotificationReceived: func(n *common.Notification) {
		log.Printf("NETCONF-NotificationReceived %s\n", n.XMLName.Local)
	},
	NotificationDropped: func(n *common.Notification) {
		log.Printf("NETCONF-NotificationDropped %s\n", n.XMLName.Local)
	},
	ExecuteStart: func(req common.Request, async bool) {
		log.Printf("NETCONF-ExecuteStart async:%v req:%s\n", async, req)
	},
	ExecuteDone: func(req common.Request, async bool, res *common.RPCReply, err error, d time.Duration) {
		log.Printf("NETCONF-ExecuteDone async:%v req:%s err:%v took:%dms\n", async, req, err, d.Milliseconds())
	},
}

// NoOpLoggingHooks provides set of hooks that do nothing.
var NoOpLoggingHooks = &ClientTrace{
	ConnectStart:     func(clientConfig *ssh.ClientConfig, target string) {},
	ConnectDone:      func(clientConfig *ssh.ClientConfig, target string, err error, d time.Duration) {},
	ConnectionClosed: func(target string, err error) {},
	HelloDone:        func(msg *common.HelloMessage) {},
	ReadStart:        func(p []byte) {},
	ReadDone:         func(p []byte, c int, err error, d time.Duration) {},

	WriteStart: func(p []byte) {},
	WriteDone:  func(p []byte, c int, err error, d time.Duration) {},

	Error:                func(context, target string, err error) {},
	NotificationReceived: func(n *common.Notification) {},
	NotificationDropped:  func(n *common.Notification) {},
	ExecuteStart:         func(req common.Request, async bool) {},
	ExecuteDone:          func(req common.Request, async bool, res *common.RPCReply, err error, d time.Duration) {},
}
