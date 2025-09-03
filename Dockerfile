FROM golang:alpine AS build

RUN apk add --update git

RUN mkdir -p /go/src/build

COPY src /go/src/build

WORKDIR /go/src/build

RUN GOPROXY=direct go build -o wplace-watcher

FROM alpine:latest

WORKDIR /app

COPY --from=build /go/src/build/wplace-watcher .

RUN chmod +x ./wplace-watcher

CMD [ "./wplace-watcher" ]