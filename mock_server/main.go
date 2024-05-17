package mock_server

// Package mock_server provides a mock implementation of the dxlink server.
// This is mainly useful for testing client implementations.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	dx "github.com/brojonat/godxfeed/dxclient"
	bwebsocket "github.com/brojonat/websocket"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const ValidAuthToken = "1234"

func ListenAndServe(ctx context.Context, logger *slog.Logger, addr string, handlers ...bwebsocket.MessageHandler) error {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
		logger = logger.With("service", "test-server")
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  512,
		WriteBufferSize: 512,
	}
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	r := mux.NewRouter()

	var cl bwebsocket.Client

	// add a dummy handler to reply to setup and auth messages
	handlers = append(handlers, func(c bwebsocket.Client, b []byte) {
		var mb dx.MessageBase
		err := json.Unmarshal(b, &mb)
		if err != nil {
			logger.Error("error deserializing message from client", "error", err.Error())
			return
		}
		switch mb.Type {
		case dx.MESSAGE_TYPE_KEEPALIVE:
			reply := dx.MessageKeepalive{MessageBase: dx.MessageBase{Type: dx.MESSAGE_TYPE_KEEPALIVE, Channel: 0}}
			bs, _ := reply.JSON()
			cl.Write(bs)

		case dx.MESSAGE_TYPE_AUTH:
			var m dx.MessageAuth
			if err = json.Unmarshal(b, &m); err != nil {
				logger.Error("error deserializing message from client", "error", err.Error())
				return
			}
			reply := dx.MessageAuthState{
				MessageBase: dx.MessageBase{
					Type:    dx.MESSAGE_TYPE_AUTH_STATE,
					Channel: 0},
				State: dx.AUTH_STATE_UNAUTHORIZED,
			}
			if m.Token == ValidAuthToken {
				reply.State = dx.AUTH_STATE_AUTHORIZED
			}
			bs, _ := reply.JSON()
			cl.Write(bs)

		case dx.MESSAGE_TYPE_SETUP:
			var m dx.MessageSetup
			if err = json.Unmarshal(b, &m); err != nil {
				logger.Error("error deserializing message from client", "error", err.Error())
				return
			}
			// simply echo back the setup message
			reply := m
			bs, _ := reply.JSON()
			cl.Write(bs)
		default:
			logger.Error("test server message type fallthrough", "payload", string(b))
			return
		}
	})
	r.Handle("/ws", bwebsocket.ServeWS(
		upgrader,
		bwebsocket.DefaultSetupConn,
		bwebsocket.NewClient,
		func(ctx context.Context, cf context.CancelFunc, c bwebsocket.Client) { cl = c }, // registration func
		func(c bwebsocket.Client) { cl = nil },                                           // deregistration func
		30*time.Second,
		handlers))

	// run the server
	srvExit := make(chan error)
	srv := http.Server{Addr: addr, Handler: r}
	go func() {
		srvExit <- srv.ListenAndServe()
	}()

	// handle shutdown
	select {
	case <-ctx.Done():
		return srv.Shutdown(context.Background())
	case err := <-srvExit:
		return err
	}
}
