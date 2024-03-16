# godxlink

There's no Go client library for `dxlink` so I wrote this one.

Here's the reference repo: https://github.com/dxFeed/dxLink

## JavaScript API

- [ ] Create instance of the client.
  - Struct literal
- [ ] Connect to the server.
  - Dial(string)
- [ ] Provide auth token if required by the server.
  - SetAuthToken(string)
- [ ] Open isolated channel to service within single connection.
  - OpenChannel(OpenChannelPayload)
- [ ] Send message to the channel.
  - Send(Message)
- [ ] Add subscription to the channel.
  - AddSubscription(Subscription)
- [ ] Remove subscription from the channel.
  - RemoveSubscription(Subscription)
- [ ] Receive messages from the channel
  - Have a run loop that pull from channel
- [ ] Add arbitrary message handlers
  - AddMessageHandler
- [ ] Remove arbitrary message handlers
  - RmMessageHandler
