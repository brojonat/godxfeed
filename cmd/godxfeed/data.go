package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v2"
)

func get_symbol_data(ctx *cli.Context) error {
	ctx.Context = context.WithValue(ctx.Context, ctxKeyMinimalSetup, true)
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	return writeCLIResponse(
		tts.GetSymbolData(ctx.String("symbol"), ctx.String("symbol-type")))
}

func get_option_chain(ctx *cli.Context) error {
	ctx.Context = context.WithValue(ctx.Context, ctxKeyMinimalSetup, true)
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	return writeCLIResponse(tts.GetOptionChain(ctx.String("symbol")))
}

func stream_symbol(ctx *cli.Context) error {
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	// Get symbols. Only streaming top SYMBOL_COUNT symbols for now; will have
	// to chunk subscription calls in the future since they apparently limit the
	// number you can subscribe to at once.
	syms, err := tts.GetRelatedSymbols(ctx.String("symbol"))
	if err != nil {
		return fmt.Errorf("could not get symbol data: %v", err)
	}
	if ctx.Int("symbol-count") < 1 {
		return fmt.Errorf("symbol-count must be >=1")
	}
	syms = syms[0:ctx.Int("symbol-count")]

	// stream
	c, err := tts.StreamSymbols(ctx.Context, syms)
	if err != nil {
		return fmt.Errorf("could not get stream symbol data: %w", err)
	}
	for b := range c {
		fmt.Printf("%s\n", b)
	}
	return nil
}
