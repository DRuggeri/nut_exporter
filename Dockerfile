### STAGE 1: Build ###

FROM golang:buster as builder

RUN mkdir -p /app/src/github.com/DRuggeri/nut_exporter
ENV GOPATH /app
WORKDIR /app
COPY . /app/src/github.com/DRuggeri/nut_exporter
RUN go install github.com/DRuggeri/nut_exporter

### STAGE 2: Setup ###

FROM alpine
RUN apk add --no-cache \
  libc6-compat
COPY --from=builder /app/bin/nut_exporter /nut_exporter
RUN chmod +x /nut_exporter
ENTRYPOINT ["/nut_exporter"]
