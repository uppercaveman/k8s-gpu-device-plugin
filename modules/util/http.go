package util

type Response struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Message string      `json:"msg"`
}

func Success(data interface{}) Response {
	return Response{Code: 0, Message: "success", Data: data}
}

func Failed(code int, msg string) Response {
	return Response{Code: code, Message: msg, Data: nil}
}
