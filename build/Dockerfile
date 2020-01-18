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
ENV SOURCE_USERNAME "empty"
ENV SOURCE_PASSWORD "empty"
ENV TARGET_USERNAME "empty"
ENV TARGET_PASSWORD "empty"
ENV TOPICS ""

# TODO: not implemented
ENV VERBOSE "false"

CMD ./mqtt-mirror $SOURCE $TARGET --source-username $SOURCE_USERNAME --source-password $SOURCE_PASSWORD --target-username $TARGET_USERNAME --target-password $TARGET_PASSWORD --verbose
#CMD ["./mqtt-mirror","$SOURCE", "$TARGET","--source-username", "$SOURCE_USERNAME","--source-password", "$SOURCE_PASSWORD","--target-username", "$TARGET_USERNAME","--target-password", "$TARGET_PASSWORD","--verbose"]