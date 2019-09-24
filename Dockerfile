FROM golang:1.13
COPY *.go ./
RUN go test *.go
RUN go build -o /proxy-server ./main.go

ENV CRA_PROXY_CACHE_DIR=/cache
RUN mkdir -p /cache
VOLUME ["/cache"]

ENV CRA_PROXY_BIND=:80
EXPOSE 80

CMD ["/proxy-server"]
