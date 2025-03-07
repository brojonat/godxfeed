package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v2"
)

func publish_nats(ctx *cli.Context) error {
	nats_url := ctx.String("nats-url")
	nats_topic := ctx.StringSlice("nats-topic")
	nats_godxfeed_user := ctx.String("nats-godxfeed-user")
	nats_godxfeed_password := ctx.String("nats-godxfeed-password")

	nc, err := nats.Connect(nats_url, nats.UserInfo(nats_godxfeed_user, nats_godxfeed_password))
	if err != nil {
		return err
	}
	defer nc.Close()

	// Create a ticker for regular intervals
	interval, err := time.ParseDuration(ctx.String("interval"))
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Loop with context cancellation
	for {
		select {
		case <-ctx.Context.Done():
			return ctx.Context.Err()
		case <-ticker.C:
			// Randomly select a topic from the slice
			randomTopic := nats_topic[rand.Intn(len(nats_topic))]
			nc.Publish(randomTopic, []byte(fmt.Sprintf("%.2f", rand.NormFloat64()*10+100)))
		}
	}
}
