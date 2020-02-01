FROM golang:alpine AS builder
ENV GO111MODULE=on
WORKDIR /app
COPY . .
RUN apk update && apk add --no-cache git
RUN GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o mqtt-mirror main.go

FROM alpine
RUN apk update && apk add --no-cache ca-certificates
COPY --from=builder /app/mqtt-mirror ./mqtt-mirror
RUN chmod +x ./mqtt-mirror
ENV SOURCE ""
ENV TARGET ""
ENV TOPIC_FILTER "#"

CMD ./mqtt-mirror $SOURCE $TARGET --verbose --topic_filter $TOPIC_FILTER
