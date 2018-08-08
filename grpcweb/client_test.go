package grpcweb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/ktr0731/grpc-test/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var defaultAddr = "http://localhost:50052"

func p(s string) *string {
	return &s
}

type protoHelper struct {
	*desc.FileDescriptor

	s map[string]*descriptor.ServiceDescriptorProto
	m map[string]*dynamic.Message
}

func (h *protoHelper) getServiceByName(t *testing.T, n string) *descriptor.ServiceDescriptorProto {
	if h.s == nil {
		h.s = map[string]*descriptor.ServiceDescriptorProto{}
		for _, svc := range h.GetServices() {
			h.s[svc.GetName()] = svc.AsServiceDescriptorProto()
		}
	}
	svc, ok := h.s[n]
	if !ok {
		require.FailNowf(t, "ServiceDescriptor not found", "no such *desc.ServiceDescriptor: %s", n)
	}
	return svc
}

func (h *protoHelper) getMessageTypeByName(t *testing.T, n string) *dynamic.Message {
	if h.m == nil {
		h.m = map[string]*dynamic.Message{}
		for _, msg := range h.GetMessageTypes() {
			h.m[msg.GetName()] = dynamic.NewMessage(msg)
		}
	}
	msg, ok := h.m[n]
	if !ok {
		require.FailNowf(t, "MessageDescriptor not found", "no such *desc.MessageDescriptor: %s", n)
	}
	return msg
}

func getAPIProto(t *testing.T) *protoHelper {
	t.Helper()

	pkgs := parseProto(t, "api.proto")
	require.Len(t, pkgs, 1)

	return &protoHelper{FileDescriptor: pkgs[0]}
}

func readFile(t *testing.T, fname string) []byte {
	b, err := ioutil.ReadFile(filepath.Join("testdata", fname))
	require.NoError(t, err)
	return b
}

type stubTransport struct {
	host string
	req  *Request

	res []byte
}

func (t *stubTransport) Send(body io.Reader) (io.Reader, error) {
	return bytes.NewReader(t.res), nil
}

func newStubTransportBuilder(t *stubTransport) TransportBuilder {
	return func(host string, req *Request) Transport {
		t.host = host
		t.req = req
		return t
	}
}

// for testing
func withStubTransport(t Transport) ClientOption {
	return func(c *Client) {
		c.tb = func(host string, req *Request) Transport {
			return t
		}
	}
}

func TestClient(t *testing.T) {
	pkg := getAPIProto(t)
	service := pkg.getServiceByName(t, "Example")

	t.Run("NewClient returns new API client", func(t *testing.T) {
		client := NewClient(defaultAddr, withStubTransport(&stubTransport{}))
		assert.NotNil(t, client)
	})

	t.Run("Send an unary API", func(t *testing.T) {
		client := NewClient(defaultAddr, withStubTransport(&stubTransport{
			res: readFile(t, "unary_ktr.out"),
		}))

		in, out := pkg.getMessageTypeByName(t, "SimpleRequest"), pkg.getMessageTypeByName(t, "SimpleResponse")
		req, err := NewRequest(service, service.GetMethod()[0], in, out)
		assert.NoError(t, err)
		err = client.Send(context.Background(), req)
		assert.NoError(t, err)
	})
}

func TestClientE2E(t *testing.T) {
	defer server.New().Serve(nil, true).Stop()

	pkg := getAPIProto(t)
	service := pkg.getServiceByName(t, "Example")

	t.Run("Unary", func(t *testing.T) {
		client := NewClient("http://localhost:50051")

		in := pkg.getMessageTypeByName(t, "SimpleRequest")

		cases := []string{
			"ktr",
			"tooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooo-looooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooong-teeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeext",
		}

		for _, c := range cases {
			in.SetFieldByName("name", c)

			out := pkg.getMessageTypeByName(t, "SimpleResponse")

			req, err := NewRequest(service, service.GetMethod()[0], in, out)
			assert.NoError(t, err)
			err = client.Send(context.Background(), req)
			assert.NoError(t, err)

			expected := fmt.Sprintf("hello, %s", c)
			assert.Equal(t, expected, out.GetFieldByName("message"))
		}
	})
}
