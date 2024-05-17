package tastytrade

import (
	"fmt"
	"net/http"
)

const (
	SYMBOL_TYPE_CRYPTO          = "crypto"
	SYMBOL_TYPE_FUTURES         = "futures"
	SYMBOL_TYPE_EQUITIES        = "equities"
	SYMBOL_TYPE_OPTIONS         = "options"
	SYMBOL_TYPE_FUTURES_OPTIONS = "futures-options"
)

type ErrorInternal struct {
	Message string `json:"message"`
}

func (e ErrorInternal) Error() string {
	return e.Message
}

type ErrorBadRequest struct {
	Message string `json:"message"`
}

func (e ErrorBadRequest) Error() string {
	return e.Message
}

func addDefaultHeaders(h http.Header) {
	h.Add("User-Agent", "tastytrade-api-client/1.0")
	h.Add("Content-Type", "application/json")
	h.Add("Accept", "application/json")
}

func GetSymbologyURL(endpoint, symType, sym string) (string, error) {
	base := fmt.Sprintf("https://%s", endpoint)
	switch symType {
	case SYMBOL_TYPE_CRYPTO:
		base += "/instruments/cryptocurrencies"
	case SYMBOL_TYPE_FUTURES:
		base += "/instruments/futures"
	case SYMBOL_TYPE_EQUITIES:
		base += fmt.Sprintf("/instruments/equities/%s", sym)
	case SYMBOL_TYPE_OPTIONS:
		base += fmt.Sprintf("/option-chains/%s", sym)
	case SYMBOL_TYPE_FUTURES_OPTIONS:
		base += fmt.Sprintf("/futures-option-chains/%s", sym)
	default:
		return "", fmt.Errorf("unrecognized symbol type %s", symType)
	}
	return base, nil
}
