FROM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.4-alpine AS backend
WORKDIR /src
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /out/patchbay ./cmd/server \
    && CGO_ENABLED=0 go build -trimpath -o /out/patchbay-demo ./cmd/demo

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /out/ /app/
COPY --from=frontend /src/web/dist/ /app/web/dist/
COPY examples/ /app/examples/
USER 10001:10001
EXPOSE 8080
CMD ["/app/patchbay", "-addr", "0.0.0.0:8080", "-demo-url", "http://demo:9091"]
