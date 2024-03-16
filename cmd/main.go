package main

import (
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

func main() {

	app := &cli.App{
		Commands: []*cli.Command{
			{
				Name:    "run-test-server",
				Aliases: []string{"ts"},
				Usage:   "run the test server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "addr",
						Value: "8080",
						Usage: "port to run the server on",
					},
				},
				Action: runTestServer,
			},
		}}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
