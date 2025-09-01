FROM golang:alpine AS build

RUN apk add --update git

RUN mkdir -p /go/src/build

COPY . /go/src/build

WORKDIR /go/src/build

RUN GOPROXY=direct go build -o wplace_watch

FROM alpine:latest

WORKDIR /app

COPY --from=build /go/src/build/wplace_watch .

RUN chmod +x ./wplace_watch

CMD [ "./wplace_watch" ]