package testserver

import (
	"encoding/xml"
	"sync"
	"time"

	"github.com/damianoneill/net/v2/netconf/common"

	"github.com/damianoneill/net/v2/netconf/common/codec"

	assert "github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// SessionHandler represents the server side of an active netconf SSH session.
type SessionHandler struct {
	// t is the testing context used for handling unexpected errors.
	t assert.TestingT

	// ch is the underlying transport connection.
	ch ssh.Channel

	// The codecs used to handle client i/o
	enc *codec.Encoder
	dec *codec.Decoder

	// Serialises access to encoder (avoiding contention between sending notifications and request responses).
	encLock sync.Mutex

	// The capabilities advertised to the client.
	capabilities []string
	// The session id to be reported to the client.
	sid uint64

	// Channel used to signal successful receipt of client capabilities.
	hellochan chan bool

	// The HelloMessage sent by the connecting client.
	ClientHello *common.HelloMessage

	// startwg will be signalled when the session is started (specifically after client
	// capabilities have been received).
	startwg *sync.WaitGroup

	// The queue of handlers used to process incoming client requests.
	// If the queue is empty, a request is processed by the EchoRequestHandler
	reqHandlers []RequestHandler

	// Records executed requests.
	reqMutex sync.Mutex
	Reqs     []RPCRequest
}

// rpcRequestMessage and rpcRequest represent an RPC request from a client, where the element type of the
// request body is unknown.
type rpcRequestMessage struct {
	XMLName   xml.Name
	MessageID string     `xml:"message-id,attr"`
	Request   RPCRequest `xml:",any"`
}

// RPCRequest describes an RPC request.
type RPCRequest struct {
	XMLName xml.Name
	Body    string `xml:",innerxml"`
}

// RPCReplyMessage  and replyData represent an rpc-reply message that will be sent to a client session, where the
// element type of the reply body (i.e. the content of the data element)
// is unknown.
type RPCReplyMessage struct {
	XMLName   xml.Name          `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc-reply"`
	Errors    []common.RPCError `xml:"rpc-error,omitempty"`
	Data      replyData         `xml:"data"`
	Ok        bool              `xml:",omitempty"`
	RawReply  string            `xml:"-"`
	MessageID string            `xml:"message-id,attr"`
}
type replyData struct {
	XMLName xml.Name `xml:"data"`
	Data    string   `xml:",innerxml"`
}

// NotifyMessage defines the contents of a notification message that will be sent to a client session, where the
// element type of the notification event is unknown.
type NotifyMessage struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 notification"`
	EventTime string   `xml:"eventTime"`
	Data      string   `xml:",innerxml"`
}

// RequestHandler is a function type that will be invoked by the session handler to handle an RPC
// request.
type RequestHandler func(h *SessionHandler, req *rpcRequestMessage)

// EchoRequestHandler responds to a request with a reply containing a data element holding
// the body of the request.
var EchoRequestHandler = func(h *SessionHandler, req *rpcRequestMessage) {
	data := replyData{Data: req.Request.Body}
	reply := &RPCReplyMessage{Data: data, MessageID: req.MessageID}
	err := h.encode(reply)
	assert.NoError(h.t, err, "Failed to encode response")
}

// FailingRequestHandler replies to a request with an error.
var FailingRequestHandler = func(h *SessionHandler, req *rpcRequestMessage) {
	reply := &RPCReplyMessage{
		MessageID: req.MessageID,
		Errors: []common.RPCError{
			{Severity: "error", Message: "oops"}},
	}
	err := h.encode(reply)
	assert.NoError(h.t, err, "Failed to encode response")
}

// CloseRequestHandler closes the transport channel on request receipt.
var CloseRequestHandler = func(h *SessionHandler, req *rpcRequestMessage) {
	_ = h.ch.Close() // nolint: errcheck, gosec
}

// IgnoreRequestHandler does in nothing on receipt of a request.
var IgnoreRequestHandler = func(h *SessionHandler, req *rpcRequestMessage) {}

// SmartRequesttHandler responds to common requests with trivial content.
var SmartRequesttHandler = func(h *SessionHandler, req *rpcRequestMessage) {

	data := replyData{Data: responseFor(req)}
	reply := &RPCReplyMessage{Data: data, MessageID: req.MessageID}
	err := h.encode(reply)
	assert.NoError(h.t, err, "Failed to encode response")
}

func responseFor(req *rpcRequestMessage) string {
	switch req.Request.XMLName.Local {
	case "get":
		return `<top><sub attr="avalue"><child1>cvalue</child1><child2/></sub></top>`
	case "get-config":
		return `<top><sub attr="cfgval1"><child1>cfgval2</child1></sub></top>`
	case "edit-config":
		return `<ok/>`
	case "get-schema":
		return `module junos-rpc-vpls {
  namespace "http://yang.juniper.net/junos/rpc/vpls";

  prefix vpls;

// etc…
`
	default:
		return req.Request.Body
	}
}

func newSessionHandler(t assert.TestingT, sid uint64) *SessionHandler { // nolint: deadcode
	wg := &sync.WaitGroup{}
	wg.Add(1)
	return &SessionHandler{t: t,
		sid:          sid,
		hellochan:    make(chan bool),
		startwg:      wg,
		capabilities: common.DefaultCapabilities,
	}
}

// Handle establishes a Netconf server session on a newly-connected SSH channel.
func (h *SessionHandler) Handle(t assert.TestingT, ch ssh.Channel) {
	h.ch = ch
	h.dec = codec.NewDecoder(ch)
	h.enc = codec.NewEncoder(ch)

	wg := &sync.WaitGroup{}
	wg.Add(1)

	// Send server hello to client.
	err := h.encode(&common.HelloMessage{Capabilities: h.capabilities, SessionID: h.sid})
	assert.NoError(h.t, err, "Failed to send server hello")

	go h.handleIncomingMessages(wg)

	h.waitForClientHello()

	// Signal server has completed setup
	h.startwg.Done()

	// Wait for message handling routine to finish.
	wg.Wait()
}

// WaitStart waits until the session handler is ready.
func (h *SessionHandler) WaitStart() {
	h.startwg.Wait()
}

// SendNotification sends a notification message with the supplied body to the client.
func (h *SessionHandler) SendNotification(body string) *SessionHandler {
	nm := &NotifyMessage{EventTime: time.Now().String(), Data: body}
	err := h.encode(nm)
	assert.NoError(h.t, err, "Failed to send server notification")
	return h
}

// Close initiates session tear-down by closing the underlying transport channel.
func (h *SessionHandler) Close() {
	_ = h.ch.Close() // nolint: errcheck, gosec
}

func (h *SessionHandler) waitForClientHello() {

	// Wait for the input handler to send the client hello.
	select {
	case <-h.hellochan:
	case <-time.After(time.Duration(5) * time.Second):
	}

	assert.NotNil(h.t, h.ClientHello, "Failed to get client hello")
}

func (h *SessionHandler) handleIncomingMessages(wg *sync.WaitGroup) {

	defer wg.Done()

	// Loop, looking for a start element type of hello, rpc-reply.
	for {
		token, err := h.dec.Token()
		if err != nil {
			break
		}
		h.handleToken(token)
	}
}

func (h *SessionHandler) handleToken(token xml.Token) {
	switch token := token.(type) {
	case xml.StartElement:
		switch token.Name.Local {
		case common.NameHello.Local: // <hello>
			h.handleHello(token)

		case common.NameRPC.Local: // <rpc>
			h.handleRPC(token)

		default:
		}
	default:
	}
}

func (h *SessionHandler) handleHello(token xml.StartElement) {
	// Decode the hello element and send it down the channel to trigger the rest of the session setup.

	h.decodeElement(&h.ClientHello, &token)

	if common.PeerSupportsChunkedFraming(h.ClientHello.Capabilities) && common.PeerSupportsChunkedFraming(h.capabilities) {

		// Update the codec to use chunked framing from now.
		codec.EnableChunkedFraming(h.dec, h.enc)
	}

	h.hellochan <- true
}

func (h *SessionHandler) handleRPC(token xml.StartElement) {
	request := &rpcRequestMessage{}
	h.decodeElement(&request, &token)

	h.reqLogger(request.Request)
	reqh := h.nextReqHandler()
	reqh(h, request)
}

func (h *SessionHandler) decodeElement(v interface{}, start *xml.StartElement) {
	err := h.dec.DecodeElement(v, start)
	assert.NoError(h.t, err, "DecodeElement failed")
}

func (h *SessionHandler) nextReqHandler() (reqh RequestHandler) {
	l := len(h.reqHandlers)
	if l == 0 {
		reqh = EchoRequestHandler
	} else {
		h.reqHandlers, reqh = h.reqHandlers[1:], h.reqHandlers[0]
	}
	return
}

func (h *SessionHandler) encode(m interface{}) error {
	h.encLock.Lock()
	defer h.encLock.Unlock()

	return h.enc.Encode(m)
}

func (h *SessionHandler) reqLogger(r RPCRequest) {
	h.reqMutex.Lock()
	defer h.reqMutex.Unlock()
	h.Reqs = append(h.Reqs, r)
}

// ReqCount delivers the number of requests received by the handler.
func (h *SessionHandler) ReqCount() int {
	return len(h.Reqs)
}

// LastReq delivers the last request received by the handler, or nil if no requests have been received.
func (h *SessionHandler) LastReq() *RPCRequest {
	count := len(h.Reqs)
	if count > 0 {
		return &h.Reqs[count-1]
	}
	return nil
}
