package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/example/cm/internal/daemon"
)

// Handler is implemented by the daemon to serve API methods.
type Handler interface {
	Add(daemon.AddRequest) error
	Remove(daemon.RemoveRequest) error
	History(context.Context, daemon.HistoryOptions) ([]daemon.HistoryEntry, error)
	Diff(context.Context, daemon.DiffOptions) (string, error)
	PreviewRestore(context.Context, string) (*daemon.RestorePreview, error)
	Restore(context.Context, daemon.RestoreRequest) (*daemon.RestoreResult, error)
	SetMessage(string) error
	List() []daemon.ListEntry
}

// DaemonHandler adapts a *daemon.Daemon to the Handler interface.
type DaemonHandler struct{ d *daemon.Daemon }

// NewHandler wraps a daemon as an API Handler.
func NewHandler(d *daemon.Daemon) *DaemonHandler { return &DaemonHandler{d: d} }

func (h *DaemonHandler) Add(req daemon.AddRequest) error { return h.d.Add(req) }
func (h *DaemonHandler) Remove(req daemon.RemoveRequest) error { return h.d.Remove(req) }
func (h *DaemonHandler) History(ctx context.Context, opts daemon.HistoryOptions) ([]daemon.HistoryEntry, error) {
	return h.d.History(ctx, opts)
}
func (h *DaemonHandler) Diff(ctx context.Context, opts daemon.DiffOptions) (string, error) {
	return h.d.Diff(ctx, opts)
}
func (h *DaemonHandler) PreviewRestore(ctx context.Context, commit string) (*daemon.RestorePreview, error) {
	return h.d.PreviewRestore(ctx, commit)
}
func (h *DaemonHandler) Restore(ctx context.Context, req daemon.RestoreRequest) (*daemon.RestoreResult, error) {
	return h.d.Restore(ctx, req)
}
func (h *DaemonHandler) SetMessage(text string) error { return h.d.SetMessage(text) }

func (h *DaemonHandler) List() []daemon.ListEntry { return h.d.List() }

// Server serves the cm API over a Unix domain socket.
type Server struct {
	socket string
	h      Handler
	log    *slog.Logger
	ln     net.Listener

	wg   sync.WaitGroup
	done chan struct{}
}

// NewServer creates a Server for the given socket path and handler.
func NewServer(socket string, h Handler, log *slog.Logger) *Server {
	return &Server{socket: socket, h: h, log: log, done: make(chan struct{})}
}

// Start listens on the socket and begins serving.
func (s *Server) Start() error {
	// remove stale socket
	_ = os.Remove(s.socket)
	if err := os.MkdirAll(filepath.Dir(s.socket), 0o755); err != nil {
		return err
	}
	ln, err := ListenUnix(s.socket)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", s.socket, err)
	}
	// The daemon usually runs as root while CLI commands run as normal users.
	// Connecting to a Unix socket requires write permission on the socket file,
	// so open it up for all users to connect.
	_ = os.Chmod(s.socket, 0o777)
	s.ln = ln
	s.wg.Add(1)
	go s.acceptLoop()
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			s.log.Error("accept error", "error", err)
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serveConn(conn)
		}()
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	var req Request
	if err := dec.Decode(&req); err != nil {
		if err != io.EOF {
			_ = writeJSON(conn, ErrResponse(fmt.Errorf("bad request: %w", err)))
		}
		return
	}
	resp := s.dispatch(&req)
	_ = writeJSON(conn, resp)
}

func (s *Server) dispatch(req *Request) *Response {
	if req.Version != Version {
		return ErrResponse(fmt.Errorf("unsupported API version %d", req.Version))
	}
	ctx := context.Background()
	switch req.Method {
	case MAdd:
		var p daemon.AddRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return ErrResponse(err)
		}
		if err := s.h.Add(p); err != nil {
			return ErrResponse(err)
		}
		return OkResponse(nil)
	case MRemove:
		var p daemon.RemoveRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return ErrResponse(err)
		}
		if err := s.h.Remove(p); err != nil {
			return ErrResponse(err)
		}
		return OkResponse(nil)
	case MHistory:
		var p daemon.HistoryOptions
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return ErrResponse(err)
		}
		r, err := s.h.History(ctx, p)
		if err != nil {
			return ErrResponse(err)
		}
		return OkResponse(r)
	case MDiff:
		var p daemon.DiffOptions
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return ErrResponse(err)
		}
		r, err := s.h.Diff(ctx, p)
		if err != nil {
			return ErrResponse(err)
		}
		return OkResponse(r)
	case MMessage:
		var p string
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return ErrResponse(err)
		}
		if err := s.h.SetMessage(p); err != nil {
			return ErrResponse(err)
		}
		return OkResponse(nil)
	case MPreview:
		var p string
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return ErrResponse(err)
		}
		r, err := s.h.PreviewRestore(ctx, p)
		if err != nil {
			return ErrResponse(err)
		}
		return OkResponse(r)
	case MRestore:
		var p daemon.RestoreRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return ErrResponse(err)
		}
		r, err := s.h.Restore(ctx, p)
		if err != nil {
			return ErrResponse(err)
		}
		return OkResponse(r)
	case MList:
		r := s.h.List()
		return OkResponse(r)
	default:
		return ErrResponse(fmt.Errorf("unknown method %q", req.Method))
	}
}

// Close stops the server and removes the socket.
func (s *Server) Close() error {
	close(s.done)
	if s.ln != nil {
		_ = s.ln.Close()
	}
	s.wg.Wait()
	_ = os.Remove(s.socket)
	return nil
}
