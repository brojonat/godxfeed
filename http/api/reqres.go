package api

// package api provides the structs used to interact with the HTTP interface to
// the godxlink service

type DefaultJSONResponse struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}
