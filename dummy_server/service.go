package dummy_server

import (
	"net/http"
	"time"

	"github.com/brojonat/server-tools/stools"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	ws "github.com/rmc-floater/websocket"
	"go.uber.org/zap"
)

// FIXME: we can move this into a MockService or something
// and use it in the client testing package

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
}

type Service struct {
	*stools.BasicService
}

func (s *Service) SetupHTTPRoutes() {

	upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	// http liveness
	s.Router.Handle("/ping", stools.AdaptHandler(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		},
	)).Methods("GET")

	// websocket
	s.Router.Handle("/ws", stools.AdaptHandler(
		ws.ServeWS(
			upgrader,
			func(r *http.Request) bool { return true },
			ws.NewPumper,
			func(w ws.WSPumper) {
				s.WSManager.RegisterClient(w)
			},
			func(w ws.WSPumper) {
				s.WSManager.UnregisterClient(w)
			},
			50*time.Second,
			ws.DefaultSetupConn,
			[]func([]byte){
				func(b []byte) {
					s.logClientMsg(b)
				},
			},
		),
	)).Methods("GET")
}

func NewService(
	l *zap.Logger,
	s *http.Server,
	r *mux.Router,
	wsm ws.WSManager,
) *Service {

	bs := &stools.BasicService{}
	bs, ok := stools.AdaptService(
		bs,
		stools.WithZapLogger(l),
		stools.WithHTTPServer(s),
		stools.WithGorillaRouter(r),
		stools.WithWSManager(wsm),
	).(*stools.BasicService)

	if !ok {
		panic("bad assertion")
	}

	svc := Service{
		BasicService: bs,
	}
	return &svc
}

func (s *Service) logClientMsg(b []byte) {
	s.Logger.Sugar().Infof(string(b))
}
