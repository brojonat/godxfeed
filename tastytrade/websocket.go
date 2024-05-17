package tastytrade

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	bwebsocket "github.com/brojonat/websocket"
)

func (s *service) RegisterClient(ctx context.Context, cf context.CancelFunc, c bwebsocket.Client) {}
func (s *service) UnregisterClient(c bwebsocket.Client)                                           {}
func (s *service) HandleClientMessage(twEndpoint, twToken, dxEndpoint, dxToken string) []bwebsocket.MessageHandler {
	return []bwebsocket.MessageHandler{
		func(c bwebsocket.Client, b []byte) {
			s.Log(int(slog.LevelInfo), fmt.Sprintf("got client message: %s", b))

			var cm ClientMessage
			if err := json.Unmarshal(b, &cm); err != nil {
				s.Log(int(slog.LevelInfo), "unable to parse client message", "data", string(b))
				return
			}

			switch cm.Type {
			case CLIENT_MESSAGE_TYPE_SUBSCRIBE:
				var body struct {
					Symbol string `json:"symbol"`
				}
				if err := json.Unmarshal(cm.Body, &body); err != nil {
					s.Log(int(slog.LevelInfo), "unable to parse client body", "data", string(cm.Body))
					return
				}
				sym := strings.ToUpper(body.Symbol)

				// Run a goroutine for each symbol the client subscribes to. When the
				// client unsubscribes from this symbol, the goroutine will be cancelled.
				ctx := context.Background()
				go func() {
					ctx, cf := context.WithCancel(ctx)
					ch, err := s.StreamSymbols(ctx, dxEndpoint, dxToken, []string{sym})
					if err != nil {
						s.Log(int(slog.LevelError), fmt.Sprintf("unable to stream symbols: %v", err))
						cf()
						return
					}
					if err := s.SubscribeClient(c, cf, sym); err != nil {
						s.Log(int(slog.LevelError), fmt.Sprintf("unable to subscribe client: %v", err))
						return
					}
					for {
						select {
						case <-ctx.Done():
							return
						case b := <-ch:
							c.Write(b)
						}
					}
				}()

			case CLIENT_MESSAGE_TYPE_UNSUBSCRIBE:
				s.Log(int(slog.LevelInfo), "got unsubscribe message", "data", string(cm.Body))
				var body struct {
					Symbol string `json:"symbol"`
				}
				if err := json.Unmarshal(cm.Body, &body); err != nil {
					s.Log(int(slog.LevelInfo), "unable to parse client body", "data", string(cm.Body))
					return
				}
				sym := strings.ToUpper(body.Symbol)
				if err := s.UnsubscribeClient(c, sym); err != nil {
					s.Log(int(slog.LevelError), fmt.Sprintf("unable to unsubscribe client: %v", err))
					return
				}
			default:
				s.Log(int(slog.LevelDebug), "unhandled message type", "data", string(b))
			}
		},
	}
}
