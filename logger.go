package main

import (
	"fmt"
	"net/http"
	"time"
)

// date format reference: Mon Jan 2 15:04:05 MST 2006
var commonLogFormatDateTimeFormat = "02/Jan/2006:15:04:05 -0700"

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		begin := time.Now()
		requestLogLine := fmt.Sprintf("%s %s %s",
			request.Method,
			request.URL.Path,
			request.Proto,
		)
		recorder := &responseRecorder{ResponseWriter: responseWriter}
		next.ServeHTTP(recorder, request)

		fmt.Printf("%s - - [%s] \"%s\" %d %s DurationSeconds:%f MappedPath:%s Cache:%s\n",
			request.RemoteAddr,
			begin.Format(commonLogFormatDateTimeFormat),
			requestLogLine,
			recorder.status,

			// This may not work in all cases, but is better than breaking the
			// sendfile to count bytes in recorder.
			recorder.header.Get("Content-Length"),

			// Extra fields not in CLF
			time.Since(begin).Seconds(),
			request.URL.Path,
			recorder.header.Get("X-Cache"),
		)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	header http.Header
}

func (rr *responseRecorder) Header() http.Header {
	rr.header = rr.ResponseWriter.Header()
	return rr.header
}

func (rr *responseRecorder) WriteHeader(status int) {
	rr.status = status
	rr.ResponseWriter.WriteHeader(status)
}
