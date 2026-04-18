package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v2"
)

// publish_nats emits synthetic Quote events matching the shape the real
// dxLink ingress publishes: one JSON object per message with the minimal
// fields the timescale sink and browser consumers expect. Symbol is
// derived from the NATS topic (godxfeed.<SYMBOL>) so the payload stays
// self-describing.
func publish_nats(ctx *cli.Context) error {
	if err := requireFlags(ctx,
		"nats-url",
		"nats-godxfeed-user",
		"nats-godxfeed-password",
	); err != nil {
		return err
	}
	if len(ctx.StringSlice("nats-topic")) == 0 {
		return fmt.Errorf("--nats-topic is required (one or more)")
	}
	nc, err := nats.Connect(
		ctx.String("nats-url"),
		nats.UserInfo(ctx.String("nats-godxfeed-user"), ctx.String("nats-godxfeed-password")),
	)
	if err != nil {
		return err
	}
	defer nc.Close()

	interval, err := time.ParseDuration(ctx.String("interval"))
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	topics := ctx.StringSlice("nats-topic")
	for {
		select {
		case <-ctx.Context.Done():
			return ctx.Context.Err()
		case <-ticker.C:
			topic := topics[rand.Intn(len(topics))]
			mid := 100.0 + rand.NormFloat64()*10
			spread := 0.05 + rand.Float64()*0.10
			payload, err := json.Marshal(map[string]any{
				"eventType":   "Quote",
				"eventSymbol": symbolFromTopic(topic),
				"bidPrice":    mid - spread/2,
				"askPrice":    mid + spread/2,
				"bidSize":     100.0 + float64(rand.Intn(400)),
				"askSize":     100.0 + float64(rand.Intn(400)),
			})
			if err != nil {
				return err
			}
			if err := nc.Publish(topic, payload); err != nil {
				return err
			}
		}
	}
}

// symbolFromTopic strips the "godxfeed." prefix and returns whatever's left.
// If the topic doesn't follow that convention, return the whole thing.
func symbolFromTopic(topic string) string {
	return strings.TrimPrefix(topic, "godxfeed.")
}
