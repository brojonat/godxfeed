package api

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorInternal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e ErrorInternal) Error() string {
	return e.Message
}

type ErrorBadRequest struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e ErrorBadRequest) Error() string {
	return e.Message
}
