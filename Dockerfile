### STAGE 1: Build ###

FROM golang:1-bullseye as builder

WORKDIR /app
COPY . /app
RUN go install

### STAGE 2: Setup ###

FROM alpine
RUN apk add --no-cache \
  libc6-compat
COPY --from=builder /go/bin/nut_exporter /nut_exporter
RUN chmod +x /nut_exporter
ENTRYPOINT ["/nut_exporter"]
