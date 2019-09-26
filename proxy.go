package main

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ProxyConfig struct {
	Prefix string `json:"prefix"`
	Target string `json:"target"`
}

func ProxyPaths(configs []ProxyConfig) func(http.Handler) http.Handler {
	var proxyClient = &http.Client{
		Timeout: time.Second * 60,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			requestPath := request.URL.Path
			for _, proxyPath := range configs {
				if strings.HasPrefix(requestPath, proxyPath.Prefix) {
					urlOut, err := url.Parse(proxyPath.Target)
					if err != nil {
						doError(responseWriter, request, err)
						return
					}
					urlOut.Path = requestPath
					urlOut.RawQuery = request.URL.RawQuery
					request.URL = urlOut
					log.Printf("Dev Proxy to %s", urlOut.String())
					if err := doProxy(responseWriter, request, proxyClient); err != nil {
						log.Printf("ERROR: %s", err.Error())
						responseWriter.WriteHeader(http.StatusBadGateway)
					}
					return
				}
			}

			next.ServeHTTP(responseWriter, request)
		})
	}
}

func doProxy(clientResponseWriter http.ResponseWriter, clientRequest *http.Request, clientForUpstream *http.Client) error {
	body, err := ioutil.ReadAll(clientRequest.Body)
	clientRequest.Body.Close()
	if err != nil {
		return err
	}
	upstreamRequest, err := http.NewRequest(clientRequest.Method, clientRequest.URL.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}

	copyHeaders(clientRequest.Header, upstreamRequest.Header)
	upstreamRequest.Header.Del("Content-Length") // Allow the http lib to handle this

	upstreamResponse, err := clientForUpstream.Do(upstreamRequest)
	if err != nil {
		return err
	}
	defer upstreamResponse.Body.Close()

	copyHeaders(upstreamResponse.Header, clientResponseWriter.Header())
	clientResponseWriter.WriteHeader(upstreamResponse.StatusCode)

	_, err = io.Copy(clientResponseWriter, upstreamResponse.Body)
	return err
}
