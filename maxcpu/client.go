package maxcpu

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"strconv"
	sync "sync"
)

type Dial func() (net.Conn, error)

type Client struct {
	dial Dial
	conn net.Conn
	mu   sync.Mutex
	rw   *bufio.ReadWriter
}

func NewClient(dial Dial) (*Client, error) {
	return &Client{
		dial: dial,
	}, nil
}

func (c *Client) connect() (net.Conn, error) {
	if c.conn != nil {
		return c.conn, nil
	}
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	c.conn = conn
	c.rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	return conn, nil
}

func (c *Client) close() {
	defer func() { c.conn = nil }()
	if c.conn == nil {
		return
	}
	c.conn.Close()
	return
}

func (c *Client) Get(key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, err := c.connect()
	if err != nil {
		c.close()
		return nil, err
	}
	extendDeadline(conn)
	if err != nil {
		c.close()
		return nil, err
	}

	_, err = c.rw.WriteString("GET " + key + "\r\n")
	if err != nil {
		c.close()
		return nil, err
	}
	err = c.rw.Flush()
	if err != nil {
		c.close()
		return nil, err
	}

	b, err := readValue(c.rw.Reader)
	if err != nil {
		c.close()
		return nil, err
	}

	return b, nil
}

func readValue(r *bufio.Reader) ([]byte, error) {
	line, _, err := r.ReadLine()
	if err != nil {
		return nil, err
	}
	if len(line) == 0 {
		return nil, fmt.Errorf("unexpected response")
	}
	fields := bytes.Fields(line)
	if len(fields) < 4 || !bytes.Equal(fields[0], []byte("VALUE")) {
		return nil, errors.New("unexpected response. not VALUE")
	}

	byteLenString := fields[3]
	byteLen, err := strconv.Atoi(string(byteLenString))
	if err != nil {
		return nil, fmt.Errorf("unexpected byte: %v", err)
	}
	byteLen += 2
	buf := make([]byte, byteLen)
	readed, err := r.Read(buf)
	if err != nil {
		return nil, err
	}
	if readed != byteLen {
		return nil, fmt.Errorf("unexpected response: could not read enough data")
	}
	return buf[0 : byteLen-2], nil
}
