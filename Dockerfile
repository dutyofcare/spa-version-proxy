FROM golang:1.13 as builder
COPY *.go ./
RUN go test *.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /proxy-server .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates

COPY --from=builder /proxy-server /

ENV CRA_PROXY_CACHE_DIR=/cache
RUN mkdir -p /cache
VOLUME ["/cache"]

ENV CRA_PROXY_BIND=:80
EXPOSE 80

CMD ["/proxy-server"]
