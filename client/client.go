package client

type Client interface {
	Dial(string) error
	Send(Message) error
	AddMessageHandler(string, func(Message) error)
	RemoveMessageHandler(string)
	Run() error
}
