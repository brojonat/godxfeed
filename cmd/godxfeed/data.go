package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brojonat/godxfeed/service/api"
	"github.com/urfave/cli/v2"
)

func get_symbol_data(ctx *cli.Context) error {
	ctx.Context = context.WithValue(ctx.Context, ctxKeyMinimalSetup, true)
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	return writeCLIResponse(
		tts.GetSymbolData(
			ctx.String("tastyworks-endpoint"),
			ctx.String("session-token"),
			ctx.String("symbol"),
			ctx.String("symbol-type"),
		))
}

func get_option_chain(ctx *cli.Context) error {
	ctx.Context = context.WithValue(ctx.Context, ctxKeyMinimalSetup, true)
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	return writeCLIResponse(
		tts.GetOptionChain(
			ctx.String("tastyworks-endpoint"),
			ctx.String("session-token"),
			ctx.String("symbol"),
			ctx.Bool("nested"),
			ctx.Bool("compact"),
		))
}

func stream_symbol(ctx *cli.Context) error {
	ctx.Context = context.WithValue(ctx.Context, ctxKeyMinimalSetup, true)
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	// Get symbols. Only streaming top SYMBOL_COUNT symbols for now; will have to chunk
	// subscription calls in the future.
	syms, err := tts.GetRelatedSymbols(
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("symbol"),
	)
	if err != nil {
		return fmt.Errorf("could not get symbol data: %v", err)
	}
	if ctx.Int("symbol-count") < 1 {
		return fmt.Errorf("symbol-count must be >=1")
	}
	syms = syms[0:ctx.Int("symbol-count")]

	// get streamer credentials
	twr, err := tts.NewStreamerToken(ctx.String("tastyworks-endpoint"), ctx.String("session-token"))
	if err != nil {
		return fmt.Errorf("could not get streamer token: %w", err)
	}
	var dxData api.TokenData
	if err := json.Unmarshal(twr.Data, &dxData); err != nil {
		return fmt.Errorf("could not unmarshal dx token data: %w", err)
	}

	// stream
	// FIXME: TastyWorks isn't actually serving the options data just yet, so the vanilla
	// symbol will have to do for now
	c, err := tts.StreamSymbols(ctx.Context, dxData.DXLinkURL, dxData.Token, syms)
	if err != nil {
		return fmt.Errorf("could not get stream symbol data: %w", err)
	}
	for b := range c {
		fmt.Printf("%s\n", b)
	}
	return nil
}
