# syntax=docker/dockerfile:1

FROM node:24 AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.26 AS go-build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN go build -trimpath -ldflags="-s -w" -o /out/context-atlas ./cmd/context-atlas

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.source="https://github.com/supercakecrumb/context-atlas"
WORKDIR /app
ENV WEB_DIST_DIR=/app/dist
ENV REFERENCE_DIR=/app/assets/reference
COPY --from=go-build --chown=nonroot:nonroot /out/context-atlas /app/context-atlas
COPY --from=web-build --chown=nonroot:nonroot /src/web/dist /app/dist
COPY --from=go-build --chown=nonroot:nonroot /src/assets/reference /app/assets/reference
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/app/context-atlas", "--healthcheck"]
USER nonroot:nonroot
ENTRYPOINT ["/app/context-atlas"]
