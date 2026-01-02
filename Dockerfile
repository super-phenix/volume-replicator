ARG PROJECT=volume-replicator

FROM golang:1.25.5-trixie AS build

ARG PROJECT
ARG USER=$PROJECT
ARG UID=1000

RUN adduser               \
  --disabled-password     \
  --gecos ""              \
  --home "/nonexistent"   \
  --shell "/sbin/nologin" \
  --no-create-home        \
  --uid $UID              \
  $USER

WORKDIR $GOPATH/src/$PROJECT/
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags '-w -s' -o /bin/main main.go

FROM scratch

ARG PROJECT
WORKDIR /

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group

WORKDIR /app

COPY --from=build /bin/main volume-replicator

USER $USER:$USER

ENTRYPOINT ["/app/volume-replicator"]



