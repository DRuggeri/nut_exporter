### STAGE 1: Build ###

FROM golang:1-alpine as builder
RUN apk add --no-cache git

WORKDIR /app
COPY . /app
RUN go install

### STAGE 2: Setup ###

FROM alpine
COPY --from=builder /go/bin/nut_exporter /nut_exporter
RUN chmod +x /nut_exporter
ENTRYPOINT ["/nut_exporter"]
