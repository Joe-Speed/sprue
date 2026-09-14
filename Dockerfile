FROM golang:1.26-alpine AS build
WORKDIR /src
COPY platform/go.mod platform/go.sum ./
RUN go mod download
COPY platform .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /sprue .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
	&& addgroup -S -g 10001 sprue \
	&& adduser -S -u 10001 -G sprue -h /app sprue \
	&& mkdir -p /data && chown sprue:sprue /data
WORKDIR /app
COPY --from=build /sprue ./sprue
ENV SPRUE_DATA=/data
EXPOSE 8080
# The process runs as an unprivileged user. On Railway the mounted volume
# must be handed to the same id: set RAILWAY_RUN_UID=10001 on the service.
USER sprue
CMD ["/app/sprue"]
