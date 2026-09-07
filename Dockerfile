FROM golang:1.26-alpine AS build
WORKDIR /src
COPY platform/go.mod platform/go.sum ./
RUN go mod download
COPY platform .
RUN CGO_ENABLED=0 go build -o /sprue .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /sprue ./sprue
ENV SPRUE_DATA=/data
EXPOSE 8080
CMD ["/app/sprue"]
