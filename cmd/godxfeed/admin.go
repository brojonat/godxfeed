package main

import (
	"context"

	"github.com/urfave/cli/v2"
)

type contextKey int

const ctxKeyMinimalSetup contextKey = 1

func new_session_token(ctx *cli.Context) error {
	ctx.Context = context.WithValue(ctx.Context, ctxKeyMinimalSetup, true)
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	return writeCLIResponse(
		tts.NewSessionToken(
			ctx.String("tastyworks-endpoint"),
			ctx.String("username"),
			ctx.String("password"),
		))
}

func dxlink_api_token(ctx *cli.Context) error {
	ctx.Context = context.WithValue(ctx.Context, ctxKeyMinimalSetup, true)
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	return writeCLIResponse(
		tts.NewStreamerToken(
			ctx.String("tastyworks-endpoint"),
			ctx.String("session-token"),
		))
}
