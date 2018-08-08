package grpcweb

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/jhump/protoreflect/desc"
	"github.com/pkg/errors"
)

type ClientOption func(*Client)

type Client struct {
	host string

	tb TransportBuilder
}

func NewClient(host string, opts ...ClientOption) *Client {
	c := &Client{
		host: host,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.tb == nil {
		c.tb = DefaultTransportBuilder
	}

	return c
}

func (c *Client) Unary(ctx context.Context, req *Request) error {
	return c.unary(ctx, req)
}

func (c *Client) unary(ctx context.Context, req *Request) error {
	b, err := proto.Marshal(req.in)
	if err != nil {
		return errors.Wrap(err, "failed to marshal the request body")
	}

	r, err := parseRequestBody(b)
	if err != nil {
		return errors.Wrap(err, "failed to build the request body")
	}

	res, err := c.tb(c.host, req).Send(ctx, r)
	if err != nil {
		return errors.Wrap(err, "failed to send the request")
	}
	defer res.Close()

	resBody, err := parseResponseBody(res, req.outDesc.GetFields())
	if err != nil {
		return errors.Wrap(err, "failed to build the response body")
	}

	if err := proto.Unmarshal(resBody, req.out); err != nil {
		return errors.Wrap(err, "failed to unmarshal response body")
	}

	return nil
}

type ServerStreamClient struct {
	ctx context.Context
	t   Transport
	req *Request

	reqOnce   sync.Once
	resStream io.ReadCloser
}

func (c *ServerStreamClient) Recv() (proto.Message, error) {
	var err error
	c.reqOnce.Do(func() {
		var b []byte
		b, err = proto.Marshal(c.req.in)
		if err != nil {
			return
		}

		var r io.Reader
		r, err = parseRequestBody(b)
		if err != nil {
			return
		}

		c.resStream, err = c.t.Send(c.ctx, r)
		if err != nil {
			return
		}
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to request server stream")
	}

	resBody, err := parseResponseBody(c.resStream, c.req.outDesc.GetFields())
	if err == io.EOF {
		return nil, err
	}

	if err != nil {
		return nil, errors.Wrap(err, "failed to build the response body")
	}

	// check compressed flag.
	// compressed flag is 0 or 1.
	if resBody[0]>>3 != 0 && resBody[0]>>3 != 1 {
		return nil, io.EOF
	}

	if err := proto.Unmarshal(resBody, c.req.out); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}

	return c.req.out, nil
}

func (c *Client) ServerStreaming(ctx context.Context, req *Request) (*ServerStreamClient, error) {
	return &ServerStreamClient{
		ctx: ctx,
		t:   c.tb(c.host, req),
		req: req,
	}, nil
}

// copied from rpc_util.go#msgHeader
const headerLen = 5

func header(body []byte) []byte {
	h := make([]byte, 5)
	h[0] = byte(0)
	binary.BigEndian.PutUint32(h[1:], uint32(len(body)))
	return h
}

// header (compressed-flag(1) + message-length(4)) + body
// TODO: compressed message
func parseRequestBody(body []byte) (io.Reader, error) {
	buf := bytes.NewBuffer(make([]byte, 0, headerLen+len(body)))
	buf.Write(header(body))
	buf.Write(body)
	return buf, nil
}

// TODO: compressed message
// copied from rpc_util#parser.recvMsg
func parseResponseBody(resBody io.Reader, fields []*desc.FieldDescriptor) ([]byte, error) {
	var h [5]byte
	if _, err := resBody.Read(h[:]); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(h[1:])
	if length == 0 {
		return nil, nil
	}

	// TODO: check message size

	content := make([]byte, int(length))
	if _, err := resBody.Read(content); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}

	return content, nil
}

func WithTransportBuilder(b TransportBuilder) ClientOption {
	return func(c *Client) {
		c.tb = b
	}
}
