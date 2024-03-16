package main

import (
	"context"
	"fmt"
	"net/http"

	"brojonat.com/godxfeed/dummy_server"
	"github.com/gorilla/mux"
	ws "github.com/rmc-floater/websocket"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

func runTestServer(cCtx *cli.Context) error {
	ctx := context.Background()
	addr := fmt.Sprintf(":%s", cCtx.String("addr"))
	l, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	r := mux.NewRouter()
	s := &http.Server{
		Handler: r,
		Addr:    addr,
	}
	wsm := ws.NewManager()
	service := dummy_server.NewService(
		l, s, r, wsm,
	)
	service.SetupHTTPRoutes()
	go service.WSManager.Run(ctx)

	service.Logger.Sugar().Infof("listening on %s", addr)
	return service.ListenAndServe()
}
