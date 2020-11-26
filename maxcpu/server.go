package maxcpu

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

// version by Makefile
var version string

var idleTimeout = 5 * time.Second

type Callback func(keys []string) (*Response, error)

type CallbackSet struct {
	cmd    string
	cb     Callback
	args   []string
	values bool
}

type Response struct {
	Response string
	Values   []*Value
}

type Value struct {
	Data []byte
	Key  string
	Flag string
}

type Server struct {
	commands map[string]Callback
}

var NotFound = &Response{
	Response: "NOT_FOUND",
}

func quitCmd(keys []string) (*Response, error) {
	return nil, io.EOF
}

func versionCmd(keys []string) (*Response, error) {
	return &Response{
		Response: "VERSION " + version,
	}, nil
}

func NewServer() (*Server, error) {
	commands := map[string]Callback{
		"QUIT":    quitCmd,
		"VERSION": versionCmd,
	}
	return &Server{
		commands: commands,
	}, nil
}

func (cs *Server) Register(cmd string, cb Callback) {
	cmd = strings.ToUpper(cmd)
	cs.commands[cmd] = cb
}

func (cs *Server) Start(ctx context.Context, l net.Listener) error {

	go func() {
		<-ctx.Done()
		if err := l.Close(); err != nil {
			log.Print(err)
		}
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				log.Printf("Shutting down")
				return nil
			default:
				log.Printf("accept error: %s", err)
				return err
			}
		}
		go cs.handleConn(ctx, conn)
	}
}

func extendDeadline(conn net.Conn) (time.Time, error) {
	d := time.Now().Add(idleTimeout)
	return d, conn.SetDeadline(d)
}

func (cs *Server) handleConn(ctx context.Context, conn net.Conn) {
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx2.Done()
		conn.Close()
	}()

	extendDeadline(conn)

	bufReader := bufio.NewReader(conn)
	scanner := bufio.NewScanner(bufReader)
	w := bufio.NewWriter(conn)

	var deadline time.Time

	for scanner.Scan() {
		var err error
		deadline, err = extendDeadline(conn)
		if err != nil {
			log.Printf("set deadline error: %s", err)
			return
		}
		cmdset, err := cs.parseCmd(scanner.Bytes())
		if err != nil {
			log.Printf("parse command error: %s", err)
			if err := cs.writeError(conn); err != nil {
				log.Printf("write error: %s", err)
				return
			}
			continue
		}
		res, err := cmdset.cb(cmdset.args)
		if err != nil {
			if err != io.EOF {
				log.Printf("execute cmd %s error: %s", cmdset.cmd, err)
				if err := cs.writeError(conn); err != nil {
					log.Printf("write error: %s", err)
					return
				}
				continue
			}
			// EOF
			return
		}
		for _, val := range res.Values {
			err := cs.writeRespose(w, val)
			if err != nil {
				if err != io.EOF {
					log.Printf("write response %s error: %s", cmdset.cmd, err)
				}
				return
			}
		}
		if cmdset.values {
			res.Response = "END"
		}
		if res.Response == "" {
			log.Printf("no message on %s: %s", cmdset.cmd, err)
			res.Response = "ERROR"
		}
		_, err = w.WriteString(res.Response)
		if err != nil {
			if err != io.EOF {
				log.Printf("write response %s error: %s", cmdset.cmd, err)
			}
			return
		}
		_, err = w.Write([]byte("\r\n"))
		if err != nil {
			if err != io.EOF {
				log.Printf("write final response %s error: %s", cmdset.cmd, err)
			}
			return
		}
		if err := w.Flush(); err != nil {
			if err != io.EOF {
				log.Printf("flush response %s error: %s", cmdset.cmd, err)
			}
			return
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case <-ctx.Done():
			// shutting down
			return
		default:
		}
		if !time.Now().After(deadline) {
			log.Printf("scanner: %s", err)
		}
	}
}

func (cs *Server) writeError(conn io.Writer) (err error) {
	_, err = conn.Write([]byte("ERROR\r\n"))
	if err != nil {
		log.Print(err)
	}
	return
}

func (cs *Server) writeRespose(w *bufio.Writer, val *Value) error {
	// VALUE <key> <flags> <bytes> [<cas unique>]\r\n
	flag := val.Flag
	if flag == "" {
		flag = "0"
	}
	_, err := w.WriteString(fmt.Sprintf("VALUE %s %s %d\r\n", val.Key, flag, len(val.Data)))
	if err != nil {
		return err
	}
	_, err = w.Write(val.Data)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte("\r\n"))
	if err != nil {
		return err
	}
	return nil
}

func (cs *Server) parseCmd(b []byte) (*CallbackSet, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("No command")
	}

	args := strings.Fields(string(b))
	name := strings.ToUpper(args[0])
	values := false
	if name == "GET" || name == "GETS" {
		name = "GET"
		values = true
	}
	if cb, ok := cs.commands[name]; ok {
		return &CallbackSet{
			cmd:    name,
			cb:     cb,
			args:   args[1:],
			values: values,
		}, nil
	}
	return nil, fmt.Errorf("Unknown command: %s", name)
}
