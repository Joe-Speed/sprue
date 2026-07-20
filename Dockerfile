FROM golang:1.26-alpine AS build
WORKDIR /src
COPY platform/go.mod platform/go.sum ./
RUN go mod download
COPY platform .
RUN CGO_ENABLED=0 go build -o /benchtime .

FROM alpine:3.22
WORKDIR /app
COPY --from=build /benchtime ./benchtime
ENV BENCHTIME_DATA=/data
EXPOSE 8080
CMD ["/app/benchtime"]
