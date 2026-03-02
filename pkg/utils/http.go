package utils

import (
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"moul.io/http2curl"
)

func Request(method string, url string, headers map[string]string, payload io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		req.Header.Add(k, v)
	}

	if curlCmd, curlErr := http2curl.GetCurlCommand(req); curlErr == nil {
		zap.L().Debug("HTTP request as curl", zap.String("cmd", curlCmd.String()))
	} else {
		zap.L().Debug("send HTTP request", zap.String("URL", url), zap.String("method", method), zap.Error(curlErr))
	}

	client := getClient()
	return client.Do(req)

}

func RequestWithData(method string, url string, headers map[string]string, payload io.Reader) ([]byte, error) {
	response, err := Request(method, url, headers, payload)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

func getClient() *http.Client {
	return &http.Client{Timeout: time.Second * 60}
}
