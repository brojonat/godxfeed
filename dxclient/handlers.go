package dxclient

import "encoding/json"

type MessageHandler func(Message)

type MessageHandlerAdapter func(MessageHandler) MessageHandler

func AdaptHandler(h MessageHandler, opts ...MessageHandlerAdapter) MessageHandler {
	for _, opt := range opts {
		h = opt(h)
	}
	return h
}

// AdaptWithType returns an adapter that calls the handler only
// if the supplied message has type t.
func AdaptWithType(t string) MessageHandlerAdapter {
	return func(h MessageHandler) MessageHandler {
		return func(m Message) {
			b, err := m.JSON()
			if err != nil {
				return
			}
			var mb MessageBase
			if err := json.Unmarshal(b, &mb); err != nil {
				return
			}
			if mb.Type != t {
				return
			}
			h(m)
		}
	}
}

// AdaptWithChannel returns an adapter that calls the handler only
// if the supplied message is on the supplied channel
func AdaptWithChannel(c int) MessageHandlerAdapter {
	return func(h MessageHandler) MessageHandler {
		return func(m Message) {
			b, err := m.JSON()
			if err != nil {
				return
			}
			var mb MessageBase
			if err := json.Unmarshal(b, &mb); err != nil {
				return
			}
			if mb.Channel != c {
				return
			}
			h(m)
		}
	}
}
