package http

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/brojonat/godxfeed/service"
	"github.com/brojonat/godxfeed/service/db/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	PlotKindRidgeLine string = "ridgeline"
)

var ridgelinePlotTemplate *template.Template

type ridgelinePlotTemplateData struct {
	Endpoint                 string
	PlotKind                 string
	LocalStorageAuthTokenKey string
	Symbol                   string
}

func handleGetPlots(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pk := r.URL.Query().Get("plot_kind")
		symbol := r.URL.Query().Get("symbol")
		if symbol == "" {
			writeBadRequestError(w, fmt.Errorf("must supply symbol"))
			return
		}
		switch pk {
		// pretty much all plots should be served by this same template
		case PlotKindRidgeLine:
			data := ridgelinePlotTemplateData{
				Endpoint:                 os.Getenv("GODXFEED_ENDPOINT"),
				LocalStorageAuthTokenKey: os.Getenv("LOCAL_STORAGE_AUTH_TOKEN_KEY"),
				PlotKind:                 pk,
				Symbol:                   symbol,
			}
			w.WriteHeader(http.StatusOK)
			err := ridgelinePlotTemplate.Execute(w, data)
			if err != nil {
				s.Log(int(slog.LevelError), "Error rendering template", "error", err)
				writeInternalError(s, w, err)
				return
			}
		default:
			writeBadRequestError(w, fmt.Errorf("unsupported plot_kind %s", pk))
			return
		}

	}
}

// Accepts a string indicating the expiry. The string should have format
// matching the *dxlink streaming symbol* format of YYMMDD, which is NOT the
// same as the usual option symbol specification. We may want to reconsider this
// in the future, but we need to do this conversion anyway; whether we want to
// do it here or before writing to the DB is a separate concern.
func parseExpiry(expqp string) (string, error) {
	if expqp == "" {
		return "", fmt.Errorf("must supply a expiry query param")
	}
	if len(expqp) != 6 {
		return "", fmt.Errorf("expiry query param must have format YYMMDD")
	}
	d, err := time.Parse("060102", expqp)
	if err != nil {
		return "", fmt.Errorf("could not parse expiry %s", expqp)
	}
	parsed := d.Format("060102")
	return parsed, nil
}

// Accepts string indicating a strike price and returns a string corresponding
// to the strike component of the *dxlink streaming symbol*, which is NOT
// the same as the 8 digit option symbol. We may want to reconsider this
// in the future, but we need this conversion anyway; whether we want to do
// it here or before writing to the DB is a separate concern.
func parseStrike(sqp string) (string, error) {
	if sqp == "" {
		return "", fmt.Errorf("must supply a strike query param")
	}
	var parsed string
	if strings.Contains(sqp, ".") {
		n, err := strconv.ParseFloat(sqp, 64)
		if err != nil {
			return "", err
		}
		// FIXME: I think this is right but need to verify the format for
		// the dxlink fractional strike prices
		parsed = fmt.Sprintf("%.1f", n)
	} else {
		n, err := strconv.ParseInt(sqp, 10, 64)
		if err != nil {
			return "", err
		}
		parsed = fmt.Sprintf("%d", n)
	}
	return parsed, nil
}

func handleGetPlotDummyData(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sym := r.URL.Query().Get("symbol")
		if sym == "" {
			writeBadRequestError(w, fmt.Errorf("must supply symbol"))
			return
		}
		// Callers can indicate either "expiry" or "strike" via the "group"
		// query parameter. This indicates that they want to fetch all the data
		// for a fixed expiry or strike, respectively.
		var symregexp string
		switch r.URL.Query().Get("group") {
		case "expiry":
			expiry := r.URL.Query().Get("expiry")
			expiry, err := parseExpiry(expiry)
			if err != nil {
				writeBadRequestError(w, err)
				return
			}
			symregexp = fmt.Sprintf(".%s%s(C|P)[0-9]{3}", sym, expiry)
		case "strike":
			strike := r.URL.Query().Get("strike")
			strike, err := parseStrike(strike)
			if err != nil {
				writeBadRequestError(w, err)
				return
			}
			symregexp = fmt.Sprintf(".%s[0-9]{6}(C|P)%s", sym, strike)
		default:
			symregexp = fmt.Sprintf(".%s[0-9]{6}(C|P)[0-9]{3}", sym)
		}
		// callers can specify the call_put query parameter as C or P to
		// indicated that they want only call or put option symbols
		cp := r.URL.Query().Get("call_put")
		if cp != "" {
			cp = strings.ToUpper(cp)
			if cp != "C" && cp != "P" {
				writeBadRequestError(w, fmt.Errorf("call_put must be be either C or P"))
				return
			}
			symregexp = strings.Replace(symregexp, "{C|P}", "{"+cp+"}", 1)
		}
		// get the recent prices for those symbols. this data is for exploratory plot
		// development, but it should mirror the format that the WS handler will eventually
		// send, which is a blob of rows, followed by individual rows it will fetch
		// after notifications from LISTEN/NOTIFY.
		// FIXME: this api seems...potentially problematic.
		rows, err := s.DBQ().GetSymbolDataRaw(r.Context(), dbgen.GetSymbolDataRawParams{
			Symregexp: symregexp,
			TsStart:   pgtype.Timestamptz{Time: time.Time{}, Valid: true},
			TsEnd:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
		if err != nil {
			writeInternalError(s, w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(rows)
	}
}
