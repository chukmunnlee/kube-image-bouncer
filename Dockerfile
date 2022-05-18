FROM golang:1.8-alpine

COPY . /go/src/github.com/kainlite/kube-image-bouncer
WORKDIR /go/src/github.com/kainlite/kube-image-bouncer
RUN go build

FROM alpine:3.15

LABEL org.opencontainers.image.source https://github.com/chukmunnlee/kube-image-bouncer

ENV BOUNCER_PORT=1323

WORKDIR /app
RUN adduser -h /app -D web
COPY --from=0 /go/src/github.com/kainlite/kube-image-bouncer/kube-image-bouncer /app/

## Cannot use the --chown option of COPY because it's not supported by
## Docker Hub's automated builds :/
RUN chown -R web:web *

USER web

EXPOSE ${BOUNCER_PORT}

ENTRYPOINT ["./kube-image-bouncer"]
