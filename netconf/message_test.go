package netconf

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	mocks "github.com/damianoneill/net/netconf/mocks"
	"github.com/stretchr/testify/mock"
	assert "github.com/stretchr/testify/require"
)

var (
	testLogger = log.New(os.Stderr, "logger:", log.Lshortfile)
)

const (
	endOfMessage = `]]>]]>`
)

func TestNewSession(t *testing.T) {

	ms := newMockServer()
	ms.closeConnection()

	ncs, err := testSession(ms)

	assert.NoError(t, err, "Not expecting new session to fail")
	assert.NotNil(t, ncs, "Session should be non-nil")
	assert.Equal(t, 4, ncs.ID(), "Session id not defined correctly")
	assert.NotNil(t, ms.clientHello, "Should have received hello")
	assert.Equal(t, ms.clientHello.Capabilities, DefaultCapabilities, "Did not send expected server capabilities")

	ncs.Close()
}

func TestNewSessionWithChunkedEncoding(t *testing.T) {

	ms := newMockServerWithBase(CapBase11)
	ms.closeConnection()

	ncs, err := testSession(ms)

	assert.NoError(t, err, "Not expecting new session to fail")
	assert.NotNil(t, ncs, "Session should be non-nil")
	assert.NotNil(t, ms.clientHello, "Should have received hello")
	assert.Equal(t, ms.clientHello.Capabilities, []string{CapBase11}, "Did not send expected server capabilities")

	ncs.Close()
}

func TestExecute(t *testing.T) {

	ms := newMockServer()
	ms.replyToRequests()

	ncs, _ := testSession(ms)

	reply, err := ncs.Execute(Request(`<get><response/></get>`))
	assert.NoError(t, err, "Not expecting exec to fail")
	assert.NotNil(t, reply, "Reply should be non-nil")
	assert.Equal(t, `<data><response/></data>`, reply.Data, "Reply should contain response data")
}

func TestExecuteAsync(t *testing.T) {

	ms := newMockServer()
	ms.replyToRequests()

	ncs, _ := testSession(ms)

	rch1 := make(chan *RPCReply)
	rch2 := make(chan *RPCReply)
	rch3 := make(chan *RPCReply)
	ncs.ExecuteAsync(Request(`<get><test1/></get>`), rch1)
	ncs.ExecuteAsync(Request(`<get><test2/></get>`), rch2)
	ncs.ExecuteAsync(Request(`<get><test3/></get>`), rch3)

	reply := <-rch3
	assert.Equal(t, `<data><test3/></data>`, reply.Data, "Reply should contain response data")
	reply = <-rch2
	assert.Equal(t, `<data><test2/></data>`, reply.Data, "Reply should contain response data")
	reply = <-rch1
	assert.Equal(t, `<data><test1/></data>`, reply.Data, "Reply should contain response data")
}

func TestExecuteAsyncUnfulfilled(t *testing.T) {

	ms := newMockServer()
	ms.ignoreRequest()

	ncs, _ := testSession(ms)

	rch1 := make(chan *RPCReply)
	ncs.ExecuteAsync(Request(`<get><test1/></get>`), rch1)

	reply := <-rch1
	assert.Nil(t, reply, "Reply should be nil")
}

func TestExecuteAsyncInterrupted(t *testing.T) {

	ms := newMockServer()
	ms.longRunningRequest()

	ncs, _ := testSession(ms)

	rch1 := make(chan *RPCReply)
	ncs.ExecuteAsync(Request(`<get><test1/></get>`), rch1)

	time.AfterFunc(time.Second*time.Duration(2), func() { ncs.Close() })
	reply := <-rch1
	assert.Nil(t, reply, "Reply should be nil")
}

func TestSubscribe(t *testing.T) {

	ms := newMockServer()
	ms.replyToNRequests(1)
	ms.sendMessage(notificationMessage())
	ms.closeConnection()

	ncs, _ := testSession(ms)

	nch := make(chan *Notification)
	reply, _ := ncs.Subscribe(Request(`<ncEvent:create-subscription xmlns:ncEvent="urn:ietf:params:xml:ns:netconf:notification:1.0"></ncEvent:create-subscription>`), nch)

	assert.NotNil(t, reply.Data, "create-subscription failed")

	result := <-nch
	assert.NotNil(t, result, "Expected notification")
	assert.Equal(t, "netconf-session-start", result.XMLName.Local, "Unexpected event type")
	assert.Equal(t, "urn:ietf:params:xml:ns:yang:ietf-netconf-notifications", result.XMLName.Space, "Unexpected event NS")
	assert.Equal(t, "2018-10-10T09:23:07Z", result.EventTime, "Unexpected event time")
	assert.Equal(t, notificationEvent(), result.Event, "Unexpected event XML")

	result = <-nch
	assert.Nil(t, result, "No more notifications expected")
}

func TestConcurrentExecute(t *testing.T) {

	ms := newMockServer()
	ms.replyToRequests()

	ncs, _ := testSession(ms)

	var wg sync.WaitGroup
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			request := fmt.Sprintf(`<get><Id_%d/></get>`, id)
			replybody := fmt.Sprintf(`<data><Id_%d/></data>`, id)
			for i := 0; i < 100; i++ {
				reply, err := ncs.Execute(Request(request))
				assert.NoError(t, err, "Not expecting exec to fail")
				assert.Equal(t, replybody, reply.Data, "Reply should contain response data")
			}
		}(r)
	}
	wg.Wait()
}

func TestConcurrentExecuteAsync(t *testing.T) {

	ms := newMockServer()
	ms.replyToRequests()

	ncs, _ := testSession(ms)

	var wg sync.WaitGroup
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			request := fmt.Sprintf(`<get><Id_%d/></get>`, id)
			replybody := fmt.Sprintf(`<data><Id_%d/></data>`, id)
			rchan := make(chan *RPCReply)
			for i := 0; i < 100; i++ {
				err := ncs.ExecuteAsync(Request(request), rchan)
				assert.NoError(t, err, "Not expecting exec to fail")
				reply := <-rchan

				assert.Equal(t, replybody, reply.Data, "Reply should contain response data")
			}
		}(r)
	}
	wg.Wait()
}

func BenchmarkExecute(b *testing.B) {

	ms := newMockServer()
	ms.replyToRequests()

	ncs, _ := testSession(ms)

	for n := 0; n < b.N; n++ {
		ncs.Execute(Request(`<get-config><source><running/></source></get-config>`))
	}
}

func BenchmarkTemplateParallel(b *testing.B) {

	ms := newMockServer()
	ms.replyToRequests()

	ncs, _ := testSession(ms)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ncs.Execute(Request(`<get-config><source><running/></source></get-config>`))
		}
	})
}

func extractRequestBody(buf []byte) string {
	re := regexp.MustCompile("<get>(.*)</get>")
	matches := re.FindStringSubmatch(string(buf))
	if len(matches) > 0 {
		return matches[1]
	}
	return ""
}

func serverHelloWithBase(base string) string {
	return `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">` +
		`<capabilities>` +
		`<capability>` +
		base +
		`</capability>` +
		`<capability>` +
		`urn:ietf:params:netconf:capability:startup:1.0` +
		`</capability>` +
		`<capability>` +
		`http://example.net/router/2.3/myfeature` +
		`</capability>` +
		`</capabilities>` +
		`<session-id>4</session-id>` +
		`</hello>`
}

func rpcReply(body string) string {
	return ` <rpc-reply message-id="101"` +
		`xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"` +
		`xmlns:ex="http://example.net/content/1.0"` +
		`ex:user-id="fred">` +
		`<data>` +
		body +
		`</data>` +
		`</rpc-reply>`
}

func notificationMessage() string {
	return `<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">` +
		`<eventTime>2018-10-10T09:23:07Z</eventTime>` +
		notificationEvent() +
		`</notification>`
}

func notificationEvent() string {
	return `<netconf-session-start xmlns="urn:ietf:params:xml:ns:yang:ietf-netconf-notifications">` +
		`<username>WRuser</username>` +
		`<session-id>321</session-id>` +
		`<source-host>172.26.136.66</source-host>` +
		`</netconf-session-start>`
}

type mockServer struct {
	transport   *mocks.Transport
	clientHello HelloMessage
	rwSynch     chan interface{}
}

func newMockServer() *mockServer {
	return newMockServerWithBase(CapBase10)
}

func newMockServerWithBase(capbase string) *mockServer {
	ms := &mockServer{transport: &mocks.Transport{}, rwSynch: make(chan interface{})}
	ms.sendMessage(serverHelloWithBase(capbase)).Once()
	ms.captureMessage(&ms.clientHello)
	ms.transport.On("Close").Return(func() error {
		close(ms.rwSynch)
		return nil
	})
	return ms
}

func (ms *mockServer) sendMessage(msg string) *mock.Call {
	return ms.transport.On("Read", mock.Anything).Return(func(buf []byte) int {
		i := []byte(msg + endOfMessage)
		copy(buf, i)
		return len(i)
	}, nil).Once()
}

func (ms *mockServer) captureMessage(msg interface{}) {
	ms.transport.On("Write", mock.Anything).Return(func(buf []byte) int {
		xml.Unmarshal(buf, msg)
		return len(buf)
	}, nil).Once()
	// Ignore the end of message delimiter.
	ms.transport.On("Write", mock.Anything).Return(func(buf []byte) int {
		return len(buf)
	}, nil).Once()
}

func (ms *mockServer) replyToRequests() {
	ms.replyToNRequests(0)
}

func (ms *mockServer) replyToNRequests(count int) {

	call := ms.transport.On("Read", mock.Anything).Return(func(buf []byte) int {
		body := <-ms.rwSynch
		i := []byte(rpcReply(body.(string)) + endOfMessage)
		copy(buf, i)
		return len(i)
	}, nil)
	if count > 0 {
		call.Times(count)
	}

	call = ms.transport.On("Write", mock.Anything).Return(func(buf []byte) int {
		if !strings.Contains(string(buf), "]]>]]>") {
			ms.rwSynch <- extractRequestBody(buf)
		}
		return len(buf)
	}, nil)
	if count > 0 {
		call.Times(count * 2)
	}
}

func (ms *mockServer) ignoreRequest() {
	wcount := 0
	ms.transport.On("Read", mock.Anything).Return(func(buf []byte) int {
		<-ms.rwSynch
		return 0
	}, io.EOF)

	ms.transport.On("Write", mock.Anything).Return(func(buf []byte) int {
		if wcount == 0 {
			ms.rwSynch <- true
			wcount++
		}
		return len(buf)
	}, nil).Twice()
}

func (ms *mockServer) longRunningRequest() {
	wcount := 0
	ms.transport.On("Read", mock.Anything).Return(func(buf []byte) int {
		<-ms.rwSynch
		return 0
	}, io.EOF)

	ms.transport.On("Write", mock.Anything).Return(func(buf []byte) int {
		if wcount == 0 {
			time.AfterFunc(time.Minute, func() { ms.rwSynch <- true })
			wcount++
		}
		return len(buf)
	}, nil).Twice()
}

func (ms *mockServer) closeConnection() {
	ms.transport.On("Read", mock.Anything).Return(0, io.EOF)
}

func testSession(ms *mockServer) (Session, error) {
	return NewSession(ms.transport, testLogger, testLogger, defaultConfig)
}

// Simple real NE access tests

// func TestRealNewSession(t *testing.T) {

// 	sshConfig := &ssh.ClientConfig{
// 		User:            "WRuser",
// 		Auth:            []ssh.AuthMethod{ssh.Password("WRuser123")},
// 		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
// 	}

// 	tr, err := NewSSHTransport(sshConfig, fmt.Sprintf("172.26.138.57:%d", 830), "netconf")
// 	assert.NoError(t, err, "Not expecting new transport to fail")
// 	defer tr.Close()

// 	l := log.New(os.Stderr, "logger:", log.Lshortfile)
// 	ncs, err := NewSession(tr, l, l, defaultConfig)
// 	assert.NoError(t, err, "Not expecting new session to fail")
// 	assert.NotNil(t, ncs, "Session should be non-nil")

// 	var wg sync.WaitGroup
// 	for n := 0; n < 1; n++ {
// 		wg.Add(1)
// 		go func(z int) {
// 			defer wg.Done()
// 			for c := 0; c < 1; c++ {
// 				reply, err := ncs.Execute(Request(`<get/>`))
// 				assert.NoError(t, err, "Not expecting exec to fail")
// 				assert.NotNil(t, reply, "Reply should be non-nil")
// 			}
// 		}(n)
// 	}
// 	wg.Wait()
// }

// func TestRealSubscription(t *testing.T) {

// 	sshConfig := &ssh.ClientConfig{
// 		User:            "WRuser",
// 		Auth:            []ssh.AuthMethod{ssh.Password("WRuser123")},
// 		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
// 	}

// 	tr, err := NewSSHTransport(sshConfig, fmt.Sprintf("172.26.138.57:%d", 830), "netconf")
// 	assert.NoError(t, err, "Not expecting new transport to fail")
// 	defer tr.Close()

// 	l := log.New(os.Stderr, "logger:", log.Lshortfile)
// 	ncs, err := NewSession(tr, l, l, defaultConfig)
// 	assert.NoError(t, err, "Not expecting new session to fail")
// 	assert.NotNil(t, ncs, "Session should be non-nil")

// 	nchan := make(chan *Notification)
// 	reply, err := ncs.Subscribe(Request(`<ncEvent:create-subscription xmlns:ncEvent="urn:ietf:params:xml:ns:netconf:notification:1.0"></ncEvent:create-subscription>`), nchan)
// 	assert.NotNil(t, reply, "Reply should be non-nil")
// 	assert.NoError(t, err, "Not expecting exec to fail")

// 	time.AfterFunc(time.Second*2, func() {
// 		tr, err := NewSSHTransport(sshConfig, fmt.Sprintf("172.26.138.57:%d", 830), "netconf")
// 		assert.NoError(t, err, "Not expecting new transport to fail")
// 		ns, _ := NewSession(tr, l, l)
// 		ns.Close()
// 	})

// 	n := <-nchan

// 	assert.NotNil(t, n, "Reply should be non-nil")
// 	fmt.Printf("%v\n", n)

// }
